package matches

import (
	"context"
	"crypto/rand"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"cardplay/internal/game"
	"cardplay/internal/game/monopoly"
	"cardplay/internal/store"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// legacyStateFor is the previous seven-round-trip read (a read-only
// transaction plus five queries), kept to measure the single-statement
// version against it.
func legacyStateFor(ctx context.Context, db *pgxpool.Pool, g game.Game, matchID, userID string) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	if _, err := q.MatchParticipant(ctx, store.MatchParticipantParams{MatchID: matchID, UserID: userID}); err != nil {
		return err
	}
	if _, err := q.Match(ctx, matchID); err != nil {
		return err
	}
	snap, err := q.LatestSnapshot(ctx, matchID)
	if err != nil {
		return err
	}
	if _, err := g.View(game.State{SchemaVersion: int(snap.SchemaVersion), Data: snap.State}, userID); err != nil {
		return err
	}
	if _, err := q.MatchParticipants(ctx, matchID); err != nil {
		return err
	}
	if _, err := q.RecentEvents(ctx, store.RecentEventsParams{MatchID: matchID, ViewerID: userID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BenchmarkStateFor compares both reads on the busiest match in
// TEST_DATABASE_URL (run after a load test), with 64 concurrent readers
// sharing a 20-connection pool as in production.
func BenchmarkStateFor(b *testing.B) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		b.Skip("set TEST_DATABASE_URL")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		b.Fatal(err)
	}
	cfg.MaxConns = 20
	// BENCH_RTT (for example 1ms) routes connections through a local proxy
	// that delays each direction by half of it, like a database on another host.
	if rtt, err := time.ParseDuration(os.Getenv("BENCH_RTT")); err == nil && rtt > 0 {
		cfg.ConnConfig.Port = latencyProxy(b, net.JoinHostPort(cfg.ConnConfig.Host, strconv.Itoa(int(cfg.ConnConfig.Port))), rtt/2)
		cfg.ConnConfig.Host = "127.0.0.1"
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()
	var matchID, userID string
	err = pool.QueryRow(ctx, `SELECT m.id, p.user_id FROM matches m JOIN match_participants p ON p.match_id=m.id
		ORDER BY (SELECT count(*) FROM game_events e WHERE e.match_id=m.id) DESC LIMIT 1`).Scan(&matchID, &userID)
	if err != nil {
		b.Skip("no match to read: run a load test first")
	}
	m := &Module{DB: pool, Games: game.NewRegistry(monopoly.Module{}), Random: rand.Reader, TurnTimeout: time.Minute}
	g, _ := m.Games.Get("monopoly-deal", "us-01723-v1")
	b.SetParallelism(8) // 8 × GOMAXPROCS(8) = 64 concurrent readers
	b.Run("legacy-7-round-trips", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if err := legacyStateFor(ctx, pool, g, matchID, userID); err != nil {
					b.Error(err)
					return
				}
			}
		})
	})
	b.Run("single-statement", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := m.StateFor(ctx, matchID, userID, 0); err != nil {
					b.Error(err)
					return
				}
			}
		})
	})
}

// latencyProxy forwards TCP to target, delaying every chunk by oneWay in each
// direction, and returns its port.
func latencyProxy(b *testing.B, target string, oneWay time.Duration) uint16 {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = ln.Close() })
	pipe := func(dst, src net.Conn) {
		buf := make([]byte, 64<<10)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				time.Sleep(oneWay)
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				_ = dst.Close()
				return
			}
		}
	}
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			server, err := net.Dial("tcp", target)
			if err != nil {
				_ = client.Close()
				continue
			}
			go pipe(server, client)
			go pipe(client, server)
		}
	}()
	return uint16(ln.Addr().(*net.TCPAddr).Port)
}

package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Applies every migration to an empty database, rolls each one back, and
// applies them again: down migrations must restore the previous schema
// exactly, and a changed migration file must be refused.
func TestMigrationsUpDownUp(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "cardplay_migcheck_" + hex.EncodeToString(suffix)
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u, _ := url.Parse(dsn)
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	ups, _ := fs.Glob(files, "migrations/*.up.sql")
	downs, _ := fs.Glob(files, "migrations/*.down.sql")
	if len(ups) == 0 || len(ups) != len(downs) {
		t.Fatalf("%d up and %d down migrations", len(ups), len(downs))
	}
	schema := func() string {
		t.Helper()
		var s string
		err := pool.QueryRow(ctx, `SELECT coalesce(string_agg(x, E'\n' ORDER BY x), '') FROM (
			SELECT 'column '||table_name||'.'||column_name||' '||data_type||' '||is_nullable||' '||coalesce(column_default,'') AS x
			  FROM information_schema.columns WHERE table_schema='public' AND table_name<>'schema_migrations'
			UNION ALL SELECT 'index '||indexname||' '||indexdef FROM pg_indexes WHERE schemaname='public' AND tablename<>'schema_migrations'
			UNION ALL SELECT 'constraint '||conrelid::regclass||' '||conname||' '||pg_get_constraintdef(oid) FROM pg_constraint WHERE connamespace='public'::regnamespace AND conrelid<>'schema_migrations'::regclass
			UNION ALL SELECT 'trigger '||tgrelid::regclass||' '||tgname FROM pg_trigger WHERE NOT tgisinternal
			UNION ALL SELECT 'function '||proname FROM pg_proc WHERE pronamespace='public'::regnamespace
			UNION ALL SELECT 'type '||typname FROM pg_type WHERE typnamespace='public'::regnamespace AND typtype='e'
		) s`).Scan(&s)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	// Apply everything and record the resulting schema.
	if err = Migrate(ctx, pool, false); err != nil {
		t.Fatalf("initial up: %v", err)
	}
	full := schema()
	if full == "" {
		t.Fatal("schema query saw no objects")
	}
	if pending, err := Pending(ctx, pool); err != nil || pending != 0 {
		t.Fatalf("pending after up=%d %v", pending, err)
	}
	// Re-running is a no-op.
	if err = Migrate(ctx, pool, false); err != nil {
		t.Fatalf("idempotent up: %v", err)
	}

	// Walk down one migration at a time, then confirm nothing is left.
	for i := len(ups) - 1; i >= 0; i-- {
		if err = Migrate(ctx, pool, true); err != nil {
			t.Fatalf("down %s: %v", ups[i], err)
		}
		if pending, _ := Pending(ctx, pool); pending != len(ups)-i {
			t.Fatalf("after down %s pending=%d", ups[i], pending)
		}
	}
	if s := schema(); s != "" {
		t.Fatalf("objects left after all downs:\n%s", s)
	}
	if err = Migrate(ctx, pool, false); err != nil {
		t.Fatalf("second up: %v", err)
	}
	if s := schema(); s != full {
		t.Fatalf("schema differs after down/up:\nfirst:\n%s\nsecond:\n%s", full, s)
	}

	// A migration whose recorded checksum differs is refused.
	if _, err = pool.Exec(ctx, "UPDATE schema_migrations SET checksum='tampered' WHERE version=$1", ups[0]); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool, false); err == nil {
		t.Fatal("changed migration was accepted")
	}
	if pending, _ := Pending(ctx, pool); pending != 1 {
		t.Fatalf("tampered checksum pending=%d", pending)
	}
}

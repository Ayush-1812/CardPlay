// Package matches runs durable multiplayer matches on top of game modules.
//
// Every state change happens in one PostgreSQL transaction holding the match
// row lock (SELECT ... FOR UPDATE), which serializes commands per match across
// all API instances. A transaction commits the new snapshot, events, command
// record and an outbox notification together before the client is
// acknowledged, so an acknowledged command survives any crash and is never
// applied twice. PostgreSQL is the only copy of match state; notifications
// are hints that make clients refetch their own projection.
package matches

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"cardplay/internal/game"
	"cardplay/internal/httpx"
	"cardplay/internal/store"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Product timings (PRD P09). There is deliberately no turn timer.
const (
	// PresenceTimeout marks a controller disconnected when its heartbeats stop
	// (for example after a crash of the instance holding the socket).
	PresenceTimeout = 45 * time.Second
	// AbandonGrace reserves an absent seat before others may vote to abandon.
	AbandonGrace = 5 * time.Minute
)

// Module owns match lifecycle and command execution.
type Module struct {
	DB     *pgxpool.Pool
	Games  *game.Registry
	Random io.Reader // crypto/rand in production
}

// Error is a match-level refusal with an HTTP status and, for stale
// commands, the current revision so clients can resynchronize.
type Error struct {
	Code     string `json:"code"`
	Status   int    `json:"-"`
	Message  string `json:"message"`
	Revision int64  `json:"revision,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

var (
	// ErrReplaced means a newer tab or device controls this seat (PRD P08).
	ErrReplaced = &Error{Code: "CONTROLLER_REPLACED", Status: 409, Message: "This match is open in another tab or device"}
	errNotFound = &Error{Code: "NOT_FOUND", Status: 404, Message: "Match not found"}
	errOver     = &Error{Code: "MATCH_OVER", Status: 409, Message: "This match has ended"}
	errPaused   = &Error{Code: "MATCH_PAUSED", Status: 409, Message: "The match is paused until every player reconnects"}
)

func live(status string) bool { return status == "playing" || status == "paused" }

// Participant is a seat as shown to every participant.
type Participant struct {
	UserID      string     `json:"user_id"`
	Seat        int32      `json:"seat"`
	Handle      string     `json:"handle"`
	DisplayName string     `json:"display_name"`
	Connected   bool       `json:"connected"`
	AbsentSince *time.Time `json:"absent_since,omitempty"`
	AbandonVote bool       `json:"abandon_vote"`
}

// EventView is one event visible to the viewer.
type EventView struct {
	Revision  int64           `json:"revision"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// State is everything one participant may know about a match. The game view
// comes only from the game module's per-seat projection.
type State struct {
	MatchID        string        `json:"match_id"`
	RoomID         string        `json:"room_id"`
	GameID         string        `json:"game_id"`
	RulesVersion   string        `json:"rules_version"`
	Status         string        `json:"status"`
	EndReason      string        `json:"end_reason,omitempty"`
	WinnerID       *string       `json:"winner_id,omitempty"`
	EndedBy        *string       `json:"ended_by,omitempty"`
	Revision       int64         `json:"revision"`
	Version        int64         `json:"version"`
	Participants   []Participant `json:"participants"`
	CanVoteAbandon bool          `json:"can_vote_abandon"`
	VoteOpensAt    *time.Time    `json:"vote_opens_at,omitempty"`
	View           game.View     `json:"view"`
	Events         []EventView   `json:"events"`
}

// Ack confirms a durably applied command.
type Ack struct {
	Revision  int64 `json:"revision"`
	Duplicate bool  `json:"duplicate"`
}

func (m *Module) tx(ctx context.Context, fn func(q *store.Queries) error) error {
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(store.New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	return err
}

func notify(ctx context.Context, q *store.Queries, roomID, kind string) error {
	return q.Enqueue(ctx, store.EnqueueParams{RoomID: roomID, Kind: kind, Payload: []byte(`{}`)})
}

// touched records a presence, vote or status change: it moves the match
// between playing and paused as seats connect or disconnect (PRD P09) and
// notifies participants.
func touched(ctx context.Context, q *store.Queries, mt store.Match) error {
	if live(mt.Status) {
		parts, err := q.MatchParticipants(ctx, mt.ID)
		if err != nil {
			return err
		}
		want := "playing"
		for _, p := range parts {
			if p.DisconnectedAt != nil {
				want = "paused"
			}
		}
		if want != mt.Status {
			if err := q.SetMatchStatus(ctx, store.SetMatchStatusParams{ID: mt.ID, Status: want}); err != nil {
				return err
			}
			if want == "playing" {
				// Votes only apply to the absence that prompted them.
				if err := q.ClearAbandonVotes(ctx, mt.ID); err != nil {
					return err
				}
			}
			return notify(ctx, q, mt.RoomID, "match.updated")
		}
	}
	if err := q.BumpMatchVersion(ctx, mt.ID); err != nil {
		return err
	}
	return notify(ctx, q, mt.RoomID, "match.updated")
}

// end finishes or abandons a match and returns its room to the lobby with
// readiness reset, so a rematch needs everyone ready again (PRD P10).
func end(ctx context.Context, q *store.Queries, mt store.Match, status, reason string, winner, by *string) error {
	err := q.EndMatch(ctx, store.EndMatchParams{ID: mt.ID, Status: status, EndReason: pgtype.Text{String: reason, Valid: true}, WinnerID: winner, EndedBy: by})
	if err == nil {
		err = q.SetRoomStatus(ctx, store.SetRoomStatusParams{ID: mt.RoomID, Status: "waiting"})
	}
	if err == nil {
		err = q.ResetReady(ctx, mt.RoomID)
	}
	if err == nil {
		err = notify(ctx, q, mt.RoomID, "room.updated")
	}
	if err == nil {
		err = notify(ctx, q, mt.RoomID, "match.updated")
	}
	return err
}

// Subscribe makes this connection the seat's controller. Any older tab or
// device loses control (PRD P08) and the match resumes if everyone is back.
func (m *Module) Subscribe(ctx context.Context, matchID, userID string) (int64, State, error) {
	var gen int64
	err := m.tx(ctx, func(q *store.Queries) error {
		mt, err := q.LockMatch(ctx, matchID)
		if err != nil {
			return notFound(err)
		}
		if gen, err = q.TakeControl(ctx, store.TakeControlParams{MatchID: matchID, UserID: userID}); err != nil {
			return notFound(err)
		}
		return touched(ctx, q, mt)
	})
	if err != nil {
		return 0, State{}, err
	}
	st, err := m.StateFor(ctx, matchID, userID, gen)
	return gen, st, err
}

// Heartbeat keeps a controller present. It fails with ErrReplaced once a
// newer controller exists.
func (m *Module) Heartbeat(ctx context.Context, matchID, userID string, gen int64) error {
	n, err := store.New(m.DB).ControllerHeartbeat(ctx, store.ControllerHeartbeatParams{MatchID: matchID, UserID: userID, ControllerGeneration: gen})
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// The seat was marked absent (for example after late heartbeats) or a
	// newer controller exists. A returning controller resumes the match.
	returned := false
	err = m.tx(ctx, func(q *store.Queries) error {
		mt, err := q.LockMatch(ctx, matchID)
		if err != nil {
			return notFound(err)
		}
		n, err := q.ControllerReturned(ctx, store.ControllerReturnedParams{MatchID: matchID, UserID: userID, ControllerGeneration: gen})
		if err != nil || n == 0 {
			return err
		}
		returned = true
		return touched(ctx, q, mt)
	})
	if err != nil {
		return err
	}
	if !returned {
		return ErrReplaced
	}
	return nil
}

// Disconnect marks the seat absent when its controller's socket closes.
func (m *Module) Disconnect(ctx context.Context, matchID, userID string, gen int64) error {
	return m.tx(ctx, func(q *store.Queries) error {
		mt, err := q.LockMatch(ctx, matchID)
		if err != nil {
			return notFound(err)
		}
		n, err := q.ControllerDisconnected(ctx, store.ControllerDisconnectedParams{MatchID: matchID, UserID: userID, ControllerGeneration: gen})
		if err != nil || n == 0 {
			return err
		}
		return touched(ctx, q, mt)
	})
}

// StateFor builds the viewer's projection from one consistent snapshot. A
// nonzero gen also checks the caller still controls the seat.
func (m *Module) StateFor(ctx context.Context, matchID, userID string, gen int64) (State, error) {
	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	me, err := q.MatchParticipant(ctx, store.MatchParticipantParams{MatchID: matchID, UserID: userID})
	if err != nil {
		return State{}, notFound(err)
	}
	if gen != 0 && me.ControllerGeneration != gen {
		return State{}, ErrReplaced
	}
	mt, err := q.Match(ctx, matchID)
	if err != nil {
		return State{}, notFound(err)
	}
	g, ok := m.Games.Get(mt.GameID, mt.RulesVersion)
	if !ok {
		return State{}, &Error{Code: "GAME_NOT_READY", Status: 501, Message: "This rules engine is not installed"}
	}
	snap, err := q.LatestSnapshot(ctx, matchID)
	if err != nil {
		return State{}, err
	}
	view, err := g.View(game.State{SchemaVersion: int(snap.SchemaVersion), Data: snap.State}, userID)
	if err != nil {
		return State{}, err
	}
	parts, err := q.MatchParticipants(ctx, matchID)
	if err != nil {
		return State{}, err
	}
	st := State{
		MatchID: mt.ID, RoomID: mt.RoomID, GameID: mt.GameID, RulesVersion: mt.RulesVersion, Status: mt.Status,
		EndReason: mt.EndReason.String, WinnerID: mt.WinnerID, EndedBy: mt.EndedBy, Revision: mt.Revision, Version: mt.Version,
		View: view, Events: []EventView{},
	}
	var earliestAbsence *time.Time
	canVote := false
	for _, p := range parts {
		st.Participants = append(st.Participants, Participant{
			UserID: p.UserID, Seat: p.Seat, Handle: p.Handle, DisplayName: p.DisplayName,
			Connected: p.DisconnectedAt == nil, AbsentSince: p.DisconnectedAt, AbandonVote: p.AbandonVote,
		})
		if p.DisconnectedAt != nil && (earliestAbsence == nil || p.DisconnectedAt.Before(*earliestAbsence)) {
			earliestAbsence = p.DisconnectedAt
		}
		if p.UserID == userID && p.DisconnectedAt == nil {
			canVote = !p.AbandonVote
		}
	}
	if mt.Status == "paused" && earliestAbsence != nil {
		opens := earliestAbsence.Add(AbandonGrace)
		st.VoteOpensAt = &opens
		st.CanVoteAbandon = canVote && time.Now().After(opens)
	}
	events, err := q.RecentEvents(ctx, store.RecentEventsParams{MatchID: matchID, ViewerID: userID})
	if err != nil {
		return State{}, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		st.Events = append(st.Events, EventView{Revision: e.Revision, Kind: e.Kind, Payload: e.Payload, CreatedAt: e.CreatedAt})
	}
	return st, tx.Commit(ctx)
}

func requestHash(cmd game.Command) (string, json.RawMessage, error) {
	payload := json.RawMessage("{}")
	if len(cmd.Payload) > 0 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, cmd.Payload); err != nil {
			return "", nil, &Error{Code: "INVALID_REQUEST", Status: 400, Message: "Command payload must be JSON"}
		}
		payload = compact.Bytes()
	}
	sum := sha256.Sum256([]byte(cmd.Kind + "\n" + strconv.FormatInt(cmd.ExpectedRevision, 10) + "\n" + string(payload)))
	return hex.EncodeToString(sum[:]), payload, nil
}

// Execute applies one command exactly once. Order of checks: controller,
// idempotency (a retried command ID returns its recorded answer), match
// status, expected revision, then the game rules.
func (m *Module) Execute(ctx context.Context, matchID, userID string, gen int64, cmd game.Command) (Ack, error) {
	if !httpx.UUID(cmd.ID) || cmd.Kind == "" || len(cmd.Kind) > 64 {
		return Ack{}, &Error{Code: "INVALID_REQUEST", Status: 400, Message: "Commands need a UUID id, a kind and an expected revision"}
	}
	hash, payload, err := requestHash(cmd)
	if err != nil {
		return Ack{}, err
	}
	cmd.Payload = payload
	var ack Ack
	var rejection error
	err = m.tx(ctx, func(q *store.Queries) error {
		mt, err := q.LockMatch(ctx, matchID)
		if err != nil {
			return notFound(err)
		}
		me, err := q.MatchParticipant(ctx, store.MatchParticipantParams{MatchID: matchID, UserID: userID})
		if err != nil {
			return notFound(err)
		}
		if me.ControllerGeneration != gen {
			return ErrReplaced
		}
		prior, err := q.FindCommand(ctx, store.FindCommandParams{MatchID: matchID, ActorID: userID, CommandID: cmd.ID})
		if err == nil {
			if prior.RequestHash != hash {
				return &Error{Code: "IDEMPOTENCY_CONFLICT", Status: 409, Message: "This command ID was used for a different command"}
			}
			var recorded struct {
				Revision int64  `json:"revision"`
				Error    *Error `json:"error"`
			}
			if err := json.Unmarshal(prior.Result, &recorded); err != nil {
				return err
			}
			if recorded.Error != nil {
				recorded.Error.Status = 422
				rejection = recorded.Error
				return nil
			}
			ack = Ack{Revision: recorded.Revision, Duplicate: true}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if !live(mt.Status) {
			return errOver
		}
		if mt.Status == "paused" {
			return errPaused
		}
		if cmd.ExpectedRevision != mt.Revision {
			return &Error{Code: "STALE_REVISION", Status: 409, Message: "The table changed; your view has been refreshed", Revision: mt.Revision}
		}
		if _, err := q.ControllerHeartbeat(ctx, store.ControllerHeartbeatParams{MatchID: matchID, UserID: userID, ControllerGeneration: gen}); err != nil {
			return err
		}
		g, ok := m.Games.Get(mt.GameID, mt.RulesVersion)
		if !ok {
			return &Error{Code: "GAME_NOT_READY", Status: 501, Message: "This rules engine is not installed"}
		}
		snap, err := q.LatestSnapshot(ctx, matchID)
		if err != nil {
			return err
		}
		tr, err := g.Apply(ctx, game.State{SchemaVersion: int(snap.SchemaVersion), Data: snap.State}, userID, cmd)
		var rej game.Rejection
		if errors.As(err, &rej) {
			// Record the refusal so a retry of this command ID gets the same answer.
			e := &Error{Code: rej.RejectionCode(), Status: 422, Message: rej.Error(), Revision: mt.Revision}
			result, _ := json.Marshal(map[string]any{"error": e})
			rejection = e
			return q.InsertCommand(ctx, store.InsertCommandParams{MatchID: matchID, ActorID: userID, CommandID: cmd.ID, RequestHash: hash, ResultingRevision: mt.Revision, Result: result})
		}
		if err != nil {
			return err
		}
		next := mt.Revision + 1
		if err := q.InsertSnapshot(ctx, store.InsertSnapshotParams{MatchID: matchID, Revision: next, SchemaVersion: int32(tr.State.SchemaVersion), State: tr.State.Data}); err != nil {
			return err
		}
		for i, e := range tr.Events {
			var audience *string
			if !e.Public {
				a := e.AudienceUserID
				audience = &a
			}
			payload := e.Payload
			if len(payload) == 0 {
				payload = []byte(`{}`)
			}
			if err := q.InsertGameEvent(ctx, store.InsertGameEventParams{MatchID: matchID, Revision: next, EventIndex: int32(i), Kind: e.Kind, AudienceUserID: audience, Public: e.Public, Payload: payload}); err != nil {
				return err
			}
		}
		result, _ := json.Marshal(map[string]int64{"revision": next})
		if err := q.InsertCommand(ctx, store.InsertCommandParams{MatchID: matchID, ActorID: userID, CommandID: cmd.ID, RequestHash: hash, ResultingRevision: next, Result: result}); err != nil {
			return err
		}
		if err := q.AdvanceMatch(ctx, store.AdvanceMatchParams{ID: matchID, Revision: next}); err != nil {
			return err
		}
		if tr.Outcome.Finished {
			winner := tr.Outcome.WinnerUserID
			if err := end(ctx, q, mt, "finished", "won", &winner, nil); err != nil {
				return err
			}
		} else if err := notify(ctx, q, mt.RoomID, "match.updated"); err != nil {
			return err
		}
		ack = Ack{Revision: next}
		return nil
	})
	if err != nil {
		return Ack{}, err
	}
	if rejection != nil {
		return Ack{}, rejection
	}
	return ack, nil
}

// Start deals a new match for a waiting room whose members are all ready.
func (m *Module) start(ctx context.Context, q *store.Queries, roomID, actor string) (string, error) {
	room, err := q.LockRoom(ctx, roomID)
	if err != nil {
		return "", notFound(err)
	}
	member, err := q.IsMember(ctx, store.IsMemberParams{RoomID: roomID, UserID: actor})
	if err != nil {
		return "", err
	}
	if !member {
		return "", &Error{Code: "NOT_FOUND", Status: 404, Message: "Room not found"}
	}
	if room.HostID != actor {
		return "", &Error{Code: "FORBIDDEN", Status: 403, Message: "Only the host may start"}
	}
	if room.Status != "waiting" {
		return "", &Error{Code: "ROOM_STARTED", Status: 409, Message: "A match is already running"}
	}
	g, ok := m.Games.Get(room.GameID, room.RulesVersion)
	if !ok || !g.Descriptor().Playable {
		return "", &Error{Code: "GAME_NOT_READY", Status: 501, Message: "This rules engine is not installed"}
	}
	members, err := q.Members(ctx, roomID)
	if err != nil {
		return "", err
	}
	d := g.Descriptor()
	if len(members) < d.MinPlayers || len(members) > d.MaxPlayers {
		return "", &Error{Code: "NOT_ENOUGH_PLAYERS", Status: 409, Message: fmt.Sprintf("%s needs %d-%d players", d.Name, d.MinPlayers, d.MaxPlayers)}
	}
	var participants []game.Participant
	for i, mem := range members {
		if !mem.Ready {
			return "", &Error{Code: "NOT_READY", Status: 409, Message: "Everyone must be ready before the host starts"}
		}
		participants = append(participants, game.Participant{UserID: mem.ID, Seat: i})
	}
	st, err := g.New(ctx, game.Setup{Participants: participants, Random: m.Random})
	if err != nil {
		return "", err
	}
	mt, err := q.CreateMatch(ctx, store.CreateMatchParams{RoomID: roomID, GameID: room.GameID, RulesVersion: room.RulesVersion})
	if err != nil {
		return "", err
	}
	for _, p := range participants {
		if err := q.AddParticipant(ctx, store.AddParticipantParams{MatchID: mt.ID, UserID: p.UserID, Seat: int32(p.Seat)}); err != nil {
			return "", err
		}
	}
	if err := q.InsertSnapshot(ctx, store.InsertSnapshotParams{MatchID: mt.ID, Revision: 0, SchemaVersion: int32(st.SchemaVersion), State: st.Data}); err != nil {
		return "", err
	}
	// Invitations end when the room starts (PRD P03).
	if err := q.SetRoomStatus(ctx, store.SetRoomStatusParams{ID: roomID, Status: "playing"}); err != nil {
		return "", err
	}
	if err := q.RevokeRoomInvitations(ctx, roomID); err != nil {
		return "", err
	}
	if err := notify(ctx, q, roomID, "room.updated"); err != nil {
		return "", err
	}
	return mt.ID, notify(ctx, q, roomID, "match.updated")
}

// Leave abandons a live match: explicit leave during play is abandonment,
// never card redistribution, and records no winner (PRD P09).
func Leave(ctx context.Context, q *store.Queries, matchID, userID string) error {
	mt, err := q.LockMatch(ctx, matchID)
	if err != nil {
		return notFound(err)
	}
	if _, err := q.MatchParticipant(ctx, store.MatchParticipantParams{MatchID: matchID, UserID: userID}); err != nil {
		return notFound(err)
	}
	if !live(mt.Status) {
		return errOver
	}
	return end(ctx, q, mt, "abandoned", "left", nil, &userID)
}

// vote records an abandonment vote. Voting opens once a seat has been absent
// for the grace period; when every connected player agrees the match is
// abandoned with no winner (PRD P09).
func (m *Module) vote(ctx context.Context, q *store.Queries, matchID, userID string, yes bool) error {
	mt, err := q.LockMatch(ctx, matchID)
	if err != nil {
		return notFound(err)
	}
	parts, err := q.MatchParticipants(ctx, matchID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(parts, func(p store.MatchParticipantsRow) bool { return p.UserID == userID })
	if i < 0 {
		return errNotFound
	}
	if mt.Status != "paused" {
		return &Error{Code: "NOT_PAUSED", Status: 409, Message: "Voting is only possible while a player is away"}
	}
	if parts[i].DisconnectedAt != nil {
		return &Error{Code: "NOT_CONNECTED", Status: 409, Message: "Reconnect to vote"}
	}
	longAbsence := slices.ContainsFunc(parts, func(p store.MatchParticipantsRow) bool {
		return p.DisconnectedAt != nil && time.Since(*p.DisconnectedAt) >= AbandonGrace
	})
	if !longAbsence {
		return &Error{Code: "TOO_EARLY", Status: 409, Message: "Seats are reserved for 5 minutes before anyone can vote"}
	}
	if err := q.SetAbandonVote(ctx, store.SetAbandonVoteParams{MatchID: matchID, UserID: userID, AbandonVote: yes}); err != nil {
		return err
	}
	parts[i].AbandonVote = yes
	unanimous := true
	for _, p := range parts {
		if p.DisconnectedAt == nil && !p.AbandonVote {
			unanimous = false
		}
	}
	if unanimous {
		return end(ctx, q, mt, "abandoned", "voted", nil, nil)
	}
	return touched(ctx, q, mt)
}

// Sweep runs the time-based lifecycle: controllers whose heartbeats stopped
// become absent (pausing their match), all-offline matches expire after 24
// hours with no winner, and ended matches are pruned then purged after 90
// days (PRD P09, P10).
func (m *Module) Sweep(ctx context.Context) error {
	q := store.New(m.DB)
	stale, err := q.StaleControllers(ctx)
	if err != nil {
		return err
	}
	slices.Sort(stale)
	for _, id := range slices.Compact(stale) {
		if err := m.tx(ctx, func(q *store.Queries) error {
			mt, err := q.LockMatch(ctx, id)
			if err != nil {
				return err
			}
			return touched(ctx, q, mt)
		}); err != nil {
			return err
		}
	}
	expired, err := q.ExpiredMatches(ctx)
	if err != nil {
		return err
	}
	for _, id := range expired {
		if err := m.tx(ctx, func(q *store.Queries) error {
			mt, err := q.LockMatch(ctx, id)
			if err != nil || !live(mt.Status) {
				return err
			}
			return end(ctx, q, mt, "abandoned", "expired", nil, nil)
		}); err != nil {
			return err
		}
	}
	return nil
}

// Retention prunes intermediate snapshots of ended matches and deletes match
// records after 90 days (PRD P10).
func (m *Module) Retention(ctx context.Context) error {
	q := store.New(m.DB)
	if err := q.PruneEndedSnapshots(ctx); err != nil {
		return err
	}
	return q.PurgeOldMatches(ctx)
}

// AbandonForDeparture ends every live match of a user who deletes their
// account, inside the caller's transaction.
func AbandonForDeparture(ctx context.Context, q *store.Queries, userID string) error {
	ids, err := q.ActiveMatchesForUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := Leave(ctx, q, id, userID); err != nil {
			return err
		}
	}
	return nil
}

package chat

import (
	"cardplay/internal/httpx"
	"cardplay/internal/store"
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

type Module struct{ DB *pgxpool.Pool }
type Error struct {
	Code    string
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

type Input struct {
	ClientID string `json:"client_id"`
	Body     string `json:"body"`
}

func (m *Module) Send(ctx context.Context, room, actor string, in Input) (store.RoomChat, error) {
	in.Body = strings.TrimSpace(in.Body)
	if !httpx.UUID(in.ClientID) || len([]rune(in.Body)) < 1 || len([]rune(in.Body)) > 500 {
		return store.RoomChat{}, &Error{"INVALID_REQUEST", 400, "Chat needs a UUID client_id and 1-500 characters"}
	}
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return store.RoomChat{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	ok, err := q.IsMember(ctx, store.IsMemberParams{RoomID: room, UserID: actor})
	if err != nil {
		return store.RoomChat{}, err
	}
	if !ok {
		return store.RoomChat{}, &Error{"NOT_FOUND", 404, "Room not found"}
	}
	// Account-wide lock makes the persisted rate limit safe across rooms.
	if err = q.LockSocialPair(ctx, "chat:"+actor); err != nil {
		return store.RoomChat{}, err
	}
	prior, err := q.ExistingChat(ctx, store.ExistingChatParams{RoomID: room, UserID: actor, ClientID: in.ClientID})
	if err == nil {
		if prior.Body != in.Body {
			return prior, &Error{"IDEMPOTENCY_CONFLICT", 409, "This client_id was used for different text"}
		}
		return prior, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.RoomChat{}, err
	}
	count, err := q.ChatRecentCount(ctx, actor)
	if err != nil {
		return store.RoomChat{}, err
	}
	if count >= 5 {
		return store.RoomChat{}, &Error{"RATE_LIMITED", 429, "Wait before sending another message"}
	}
	msg, err := q.InsertChat(ctx, store.InsertChatParams{RoomID: room, UserID: actor, ClientID: in.ClientID, Body: in.Body})
	if err == nil {
		err = q.Enqueue(ctx, store.EnqueueParams{RoomID: room, Kind: "chat.updated", Payload: []byte(`{}`)})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return msg, err
}
func (m *Module) Post(w http.ResponseWriter, r *http.Request) {
	var in Input
	if !httpx.Decode(w, r, &in) {
		return
	}
	msg, err := m.Send(r.Context(), chi.URLParam(r, "roomID"), httpx.Actor(r).ID, in)
	var ce *Error
	if errors.As(err, &ce) {
		httpx.Error(w, r, ce.Status, ce.Code, ce.Message)
		return
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, msg)
}
func (m *Module) List(w http.ResponseWriter, r *http.Request) {
	q := store.New(m.DB)
	actor := httpx.Actor(r).ID
	id := chi.URLParam(r, "roomID")
	ok, err := q.IsMember(r.Context(), store.IsMemberParams{RoomID: id, UserID: actor})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !ok {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room not found")
		return
	}
	var after int64
	if s := r.URL.Query().Get("after"); s != "" {
		after, err = strconv.ParseInt(s, 10, 64)
		if err != nil || after < 0 {
			httpx.Error(w, r, 400, "INVALID_REQUEST", "after must be a nonnegative integer")
			return
		}
	}
	items, err := q.ChatPage(r.Context(), store.ChatPageParams{RoomID: id, AfterID: after, ViewerID: actor})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	// Without a cursor the query selects the newest page; return it oldest first.
	if after == 0 {
		slices.Reverse(items)
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}

// Report is POST /rooms/{roomID}/chat/{messageID}/report. Members may report
// another member's recent message; reports are kept for moderator review
// (PRD P06). A repeated report of the same message is accepted once.
func (m *Module) Report(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if n := len([]rune(in.Reason)); n < 1 || n > 500 {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Give a reason of 1-500 characters")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "messageID"), 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, r, 404, "NOT_FOUND", "Message not found")
		return
	}
	actor, room := httpx.Actor(r).ID, chi.URLParam(r, "roomID")
	q := store.New(m.DB)
	ok, err := q.IsMember(r.Context(), store.IsMemberParams{RoomID: room, UserID: actor})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !ok {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room not found")
		return
	}
	author, err := q.ChatAuthor(r.Context(), store.ChatAuthorParams{ID: id, RoomID: room})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, 404, "NOT_FOUND", "Message not found")
		return
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if author == actor {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "You cannot report your own message")
		return
	}
	if _, err := q.ReportChat(r.Context(), store.ReportChatParams{MessageID: id, ReporterID: actor, Reason: in.Reason}); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

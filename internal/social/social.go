package social

import (
	"cardplay/internal/httpx"
	"cardplay/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

type Module struct{ DB *pgxpool.Pool }

func Pair(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + ":" + b
}
func (m *Module) Search(w http.ResponseWriter, r *http.Request) {
	handle := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("handle")))
	if len(handle) < 3 || len(handle) > 24 {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Search by an exact 3-24 character handle")
		return
	}
	u, err := store.New(m.DB).SearchPublicUser(r.Context(), store.SearchPublicUserParams{Handle: handle, ActorID: httpx.Actor(r).ID})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, u)
}
func (m *Module) Blocks(w http.ResponseWriter, r *http.Request) {
	items, err := store.New(m.DB).ListBlocks(r.Context(), httpx.Actor(r).ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}
func (m *Module) List(w http.ResponseWriter, r *http.Request) {
	items, err := store.New(m.DB).ListFriendships(r.Context(), httpx.Actor(r).ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}
func (m *Module) Change(w http.ResponseWriter, r *http.Request) {
	actor := httpx.Actor(r).ID
	other := chi.URLParam(r, "userID")
	action := chi.URLParam(r, "action")
	if !httpx.UUID(other) || actor == other {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Choose another user")
		return
	}
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	if err = q.LockSocialPair(r.Context(), Pair(actor, other)); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	blocked, err := q.HasBlock(r.Context(), store.HasBlockParams{UserID: actor, BlockedID: other})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if blocked && (action == "request" || action == "accept") {
		httpx.Error(w, r, 404, "NOT_FOUND", "User unavailable")
		return
	}
	switch action {
	case "request":
		// Crossed requests keep a single pending pair: only the recipient accepts.
		_, err = q.RequestFriend(r.Context(), store.RequestFriendParams{RequesterID: actor, RecipientID: other})
	case "accept":
		var n int64
		n, err = q.AcceptFriend(r.Context(), store.AcceptFriendParams{RequesterID: other, RecipientID: actor})
		if err == nil && n == 0 {
			httpx.Error(w, r, 409, "CONFLICT", "No incoming request to accept")
			return
		}
	case "remove":
		err = q.RemoveFriend(r.Context(), store.RemoveFriendParams{RequesterID: actor, RecipientID: other})
	case "decline":
		var n int64
		n, err = q.DeclineFriend(r.Context(), store.DeclineFriendParams{RequesterID: other, RecipientID: actor})
		if err == nil && n == 0 {
			httpx.Error(w, r, 409, "CONFLICT", "No incoming request to decline")
			return
		}
	case "block":
		err = q.BlockUser(r.Context(), store.BlockUserParams{UserID: actor, BlockedID: other})
		if err == nil {
			err = q.RemoveFriend(r.Context(), store.RemoveFriendParams{RequesterID: actor, RecipientID: other})
		}
		if err == nil {
			err = q.RevokePairInvitations(r.Context(), store.RevokePairInvitationsParams{ActorID: actor, OtherID: other})
		}
	case "unblock":
		err = q.UnblockUser(r.Context(), store.UnblockUserParams{UserID: actor, BlockedID: other})
	default:
		httpx.Error(w, r, 404, "NOT_FOUND", "Unknown friendship action")
		return
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

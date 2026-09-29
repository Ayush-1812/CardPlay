package matches

import (
	"cardplay/internal/game"
	"cardplay/internal/httpx"
	"cardplay/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

type Module struct {
	DB    *pgxpool.Pool
	Games *game.Registry
}

func (m *Module) Start(w http.ResponseWriter, r *http.Request) {
	q := store.New(m.DB)
	id := chi.URLParam(r, "roomID")
	member, err := q.IsMember(r.Context(), store.IsMemberParams{RoomID: id, UserID: httpx.Actor(r).ID})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !member {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room not found")
		return
	}
	room, err := q.Room(r.Context(), id)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if room.HostID != httpx.Actor(r).ID {
		httpx.Error(w, r, 403, "FORBIDDEN", "Only the host may start")
		return
	}
	httpx.Error(w, r, 501, "GAME_NOT_READY", "The foundation supports lobbies; the rules engine is the next milestone")
}
func (m *Module) View(w http.ResponseWriter, r *http.Request) {
	q := store.New(m.DB)
	id := chi.URLParam(r, "matchID")
	actor := httpx.Actor(r).ID
	ok, err := q.IsParticipant(r.Context(), store.IsParticipantParams{MatchID: id, UserID: actor})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !ok {
		httpx.Error(w, r, 404, "NOT_FOUND", "Match not found")
		return
	}
	match, err := q.Match(r.Context(), id)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	g, ok := m.Games.Get(match.GameID, match.RulesVersion)
	if !ok || !g.Descriptor().Playable {
		httpx.Error(w, r, 501, "GAME_NOT_READY", "This rules engine is not installed")
		return
	}
	snapshot, err := q.LatestSnapshot(r.Context(), id)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	view, err := g.View(game.State{SchemaVersion: int(snapshot.SchemaVersion), Data: snapshot.State}, actor)
	if err != nil {
		httpx.Error(w, r, 500, "STATE_INVALID", "Unable to project this game state")
		return
	}
	httpx.JSON(w, 200, map[string]any{"match_id": id, "revision": snapshot.Revision, "rules_version": match.RulesVersion, "view": view})
}

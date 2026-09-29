package matches

import (
	"errors"
	"net/http"

	"cardplay/internal/httpx"
	"cardplay/internal/store"

	"github.com/go-chi/chi/v5"
)

// WriteError renders match errors; anything else is a database error.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var me *Error
	if errors.As(err, &me) {
		httpx.Error(w, r, me.Status, me.Code, me.Message)
		return
	}
	httpx.DBError(w, r, err)
}

// Start is POST /rooms/{roomID}/matches: host only, everyone ready.
func (m *Module) Start(w http.ResponseWriter, r *http.Request) {
	var id string
	err := m.tx(r.Context(), func(q *store.Queries) error {
		var err error
		id, err = m.start(r.Context(), q, chi.URLParam(r, "roomID"), httpx.Actor(r).ID)
		return err
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]string{"match_id": id})
}

// View is GET /matches/{matchID}: the caller's projection, for resync
// without taking control of the seat.
func (m *Module) View(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "matchID")
	if !httpx.UUID(id) {
		httpx.Error(w, r, 404, "NOT_FOUND", "Match not found")
		return
	}
	st, err := m.StateFor(r.Context(), id, httpx.Actor(r).ID, 0)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	httpx.JSON(w, 200, st)
}

// LeaveMatch is POST /matches/{matchID}/leave.
func (m *Module) LeaveMatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "matchID")
	if !httpx.UUID(id) {
		httpx.Error(w, r, 404, "NOT_FOUND", "Match not found")
		return
	}
	if err := m.tx(r.Context(), func(q *store.Queries) error { return Leave(r.Context(), q, id, httpx.Actor(r).ID) }); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

// Vote is POST /matches/{matchID}/abandon with {"vote": bool}.
func (m *Module) Vote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vote bool `json:"vote"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	id := chi.URLParam(r, "matchID")
	if !httpx.UUID(id) {
		httpx.Error(w, r, 404, "NOT_FOUND", "Match not found")
		return
	}
	if err := m.tx(r.Context(), func(q *store.Queries) error { return m.vote(r.Context(), q, id, httpx.Actor(r).ID, in.Vote) }); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

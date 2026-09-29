package rooms

import (
	"cardplay/internal/httpx"
	"cardplay/internal/store"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Host mutations take the room row lock before checking membership or writing.
// This serializes joins, seat changes, kicks and closing the lobby.
func hostWaiting(w http.ResponseWriter, r *http.Request, q *store.Queries, id string) (store.Room, bool) {
	room, err := q.LockRoom(r.Context(), id)
	if err != nil {
		httpx.DBError(w, r, err)
		return room, false
	}
	if room.HostID != httpx.Actor(r).ID {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room not found")
		return room, false
	}
	if room.Status != "waiting" {
		httpx.Error(w, r, 409, "ROOM_STARTED", "Room is not waiting")
		return room, false
	}
	return room, true
}

func (m *Module) Update(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Capacity int32  `json:"capacity"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if n := len([]rune(in.Name)); n < 1 || n > 80 || in.Capacity < 2 || in.Capacity > 5 {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Name is required; capacity must be 2-5")
		return
	}
	id := chi.URLParam(r, "roomID")
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	room, ok := hostWaiting(w, r, q, id)
	if !ok {
		return
	}
	members, err := q.Members(r.Context(), id)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if len(members) > int(in.Capacity) {
		httpx.Error(w, r, 409, "ROOM_FULL", "Capacity cannot be below the number of players")
		return
	}
	for _, member := range members {
		if member.Seat >= in.Capacity {
			httpx.Error(w, r, 409, "ROOM_FULL", "Capacity cannot exclude an occupied seat")
			return
		}
	}
	if err = q.UpdateRoom(r.Context(), store.UpdateRoomParams{ID: id, Name: in.Name, Capacity: in.Capacity}); err == nil {
		err = changed(r.Context(), q, id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	room.Name, room.Capacity, room.Revision = in.Name, in.Capacity, room.Revision+1
	httpx.JSON(w, 200, room)
}

func (m *Module) Close(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "roomID")
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	if _, ok := hostWaiting(w, r, q, id); !ok {
		return
	}
	if err = q.CloseRoom(r.Context(), id); err == nil {
		err = q.RevokeRoomInvitations(r.Context(), id)
	}
	if err == nil {
		err = changed(r.Context(), q, id)
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

func (m *Module) Kick(w http.ResponseWriter, r *http.Request) {
	id, target := chi.URLParam(r, "roomID"), chi.URLParam(r, "userID")
	actor := httpx.Actor(r).ID
	if !httpx.UUID(target) {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Choose another room member")
		return
	}
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	if _, ok := hostWaiting(w, r, q, id); !ok {
		return
	}
	if target == actor {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Choose another room member")
		return
	}
	member, err := q.IsMember(r.Context(), store.IsMemberParams{RoomID: id, UserID: target})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !member {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room member not found")
		return
	}
	if err = q.BanMember(r.Context(), store.BanMemberParams{RoomID: id, UserID: target, ActorID: actor}); err == nil {
		_, err = q.RemoveMember(r.Context(), store.RemoveMemberParams{RoomID: id, UserID: target})
	}
	if err == nil {
		err = q.RevokeTargetRoomInvitations(r.Context(), store.RevokeTargetRoomInvitationsParams{RoomID: id, TargetID: target})
	}
	if err == nil {
		err = q.ResetReady(r.Context(), id)
	}
	if err == nil {
		err = changed(r.Context(), q, id)
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

func (m *Module) TransferHost(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID string `json:"user_id"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if !httpx.UUID(in.UserID) || in.UserID == httpx.Actor(r).ID {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Choose another room member")
		return
	}
	id := chi.URLParam(r, "roomID")
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	if _, ok := hostWaiting(w, r, q, id); !ok {
		return
	}
	member, err := q.IsMember(r.Context(), store.IsMemberParams{RoomID: id, UserID: in.UserID})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !member {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room member not found")
		return
	}
	if err = q.SetRoomHost(r.Context(), store.SetRoomHostParams{ID: id, HostID: in.UserID}); err == nil {
		err = changed(r.Context(), q, id)
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

func (m *Module) Leave(w http.ResponseWriter, r *http.Request) {
	id, actor := chi.URLParam(r, "roomID"), httpx.Actor(r).ID
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	room, err := q.LockRoom(r.Context(), id)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	member, err := q.IsMember(r.Context(), store.IsMemberParams{RoomID: id, UserID: actor})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !member {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room not found")
		return
	}
	if room.Status != "waiting" {
		httpx.Error(w, r, 409, "ROOM_STARTED", "Room is not waiting")
		return
	}
	members, err := q.Members(r.Context(), id)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if room.HostID == actor {
		var next *store.MembersRow
		for i := range members {
			if members[i].ID != actor && (next == nil || members[i].JoinedAt.Before(next.JoinedAt)) {
				next = &members[i]
			}
		}
		if next != nil {
			err = q.SetRoomHost(r.Context(), store.SetRoomHostParams{ID: id, HostID: next.ID})
		} else {
			err = q.CloseRoom(r.Context(), id)
		}
		if err != nil {
			httpx.DBError(w, r, err)
			return
		}
	}
	if _, err = q.RemoveMember(r.Context(), store.RemoveMemberParams{RoomID: id, UserID: actor}); err == nil {
		err = q.ResetReady(r.Context(), id)
	}
	if err == nil {
		err = changed(r.Context(), q, id)
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

func (m *Module) CreatedInvitations(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "roomID")
	q := store.New(m.DB)
	ok, err := q.IsMember(r.Context(), store.IsMemberParams{RoomID: id, UserID: httpx.Actor(r).ID})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !ok {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room not found")
		return
	}
	items, err := q.RoomInvitationsByCreator(r.Context(), store.RoomInvitationsByCreatorParams{RoomID: id, InviterID: httpx.Actor(r).ID})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}

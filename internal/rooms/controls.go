package rooms

import (
	"fmt"

	"cardplay/internal/httpx"
	"cardplay/internal/store"
	"net/http"

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
		TeamA    string `json:"team_a"`
		TeamB    string `json:"team_b"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	var ok bool
	if in.Name, ok = httpx.CleanText(in.Name, 80); !ok {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Name is required")
		return
	}
	// Team names are optional: empty keeps the client's default label.
	for _, team := range []*string{&in.TeamA, &in.TeamB} {
		if *team == "" {
			continue
		}
		if *team, ok = httpx.CleanText(*team, 24); !ok {
			httpx.Error(w, r, 400, "INVALID_REQUEST", "Team names are 1-24 characters of plain text")
			return
		}
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
	d, known := m.describe(room.GameID)
	if !known {
		httpx.Error(w, r, 400, "UNKNOWN_GAME", "That game is not available")
		return
	}
	if int(in.Capacity) < d.MinPlayers || int(in.Capacity) > d.MaxPlayers {
		httpx.Error(w, r, 400, "INVALID_REQUEST", fmt.Sprintf("%s needs %d-%d players", d.Name, d.MinPlayers, d.MaxPlayers))
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
	if err = q.UpdateRoom(r.Context(), store.UpdateRoomParams{ID: id, Name: in.Name, Capacity: in.Capacity, TeamA: in.TeamA, TeamB: in.TeamB}); err == nil {
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
	// Rooms are temporary (owner decision 2026-10-01). Members are told the
	// room changed; they refetch it, find it gone, and return to the lobby.
	if err = changed(r.Context(), q, id); err == nil {
		err = q.DeleteRoom(r.Context(), id)
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
		// Revokes invitations to the removed member and links they created.
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

// NextHost returns the longest-present member other than the departing host,
// or nil when nobody else is seated.
func NextHost(members []store.MembersRow, departing string) *store.MembersRow {
	var next *store.MembersRow
	for i := range members {
		if members[i].ID != departing && (next == nil || members[i].JoinedAt.Before(next.JoinedAt)) {
			next = &members[i]
		}
	}
	return next
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
		if next := NextHost(members, actor); next != nil {
			if err = q.SetRoomHost(r.Context(), store.SetRoomHostParams{ID: id, HostID: next.ID}); err != nil {
				httpx.DBError(w, r, err)
				return
			}
		}
	}
	if _, err = q.RemoveMember(r.Context(), store.RemoveMemberParams{RoomID: id, UserID: actor}); err == nil {
		// Links the departing member created must not keep admitting people.
		err = q.RevokeTargetRoomInvitations(r.Context(), store.RevokeTargetRoomInvitationsParams{RoomID: id, TargetID: actor})
	}
	// Rooms are temporary (owner decision 2026-10-01): the last one out
	// deletes the room and anything left in it.
	var remaining int64
	if err == nil {
		remaining, err = q.MemberCount(r.Context(), id)
	}
	if err == nil && remaining == 0 {
		err = q.DeleteRoom(r.Context(), id)
	} else if err == nil {
		if err = q.ResetReady(r.Context(), id); err == nil {
			err = changed(r.Context(), q, id)
		}
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

// Seat moves one player to another seat, swapping with whoever is there.
// Teams in a four-seat game are decided by seat (0 and 2 against 1 and 3), so
// this is how players change team. Host only, and only before a match starts.
func (m *Module) Seat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Seat int32 `json:"seat"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	roomID, target := chi.URLParam(r, "roomID"), chi.URLParam(r, "userID")
	if !httpx.UUID(target) {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Invalid player")
		return
	}
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	room, ok := hostWaiting(w, r, q, roomID)
	if !ok {
		return
	}
	if in.Seat < 0 || in.Seat >= room.Capacity {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "That seat does not exist at this table")
		return
	}
	members, err := q.Members(r.Context(), roomID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	var mover, sitting *store.MembersRow
	for i := range members {
		if members[i].ID == target {
			mover = &members[i]
		}
		if members[i].Seat == in.Seat {
			sitting = &members[i]
		}
	}
	if mover == nil {
		httpx.Error(w, r, 404, "NOT_FOUND", "That player is not in this room")
		return
	}
	if mover.Seat == in.Seat {
		httpx.JSON(w, 200, map[string]any{"seat": in.Seat})
		return
	}
	if sitting == nil {
		// The seat is free: a plain move, no swap partner needed.
		if err = q.SwapSeats(r.Context(), store.SwapSeatsParams{RoomID: roomID, First: mover.ID, FirstSeat: in.Seat, Second: mover.ID, SecondSeat: in.Seat}); err != nil {
			httpx.DBError(w, r, err)
			return
		}
	} else {
		// Both rows change in one statement, so the seat uniqueness check
		// has to wait until the transaction commits.
		if _, err = tx.Exec(r.Context(), "SET CONSTRAINTS room_members_room_id_seat_key DEFERRED"); err != nil {
			httpx.DBError(w, r, err)
			return
		}
		if err = q.SwapSeats(r.Context(), store.SwapSeatsParams{
			RoomID: roomID,
			First:  mover.ID, FirstSeat: mover.Seat,
			Second: sitting.ID, SecondSeat: in.Seat,
		}); err != nil {
			httpx.DBError(w, r, err)
			return
		}
	}
	// Changing the table clears readiness: everyone confirms the new seating.
	if err = q.ResetReady(r.Context(), roomID); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if err = changed(r.Context(), q, roomID); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"seat": in.Seat})
}

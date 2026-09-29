package rooms

import (
	"cardplay/internal/httpx"
	"cardplay/internal/social"
	"cardplay/internal/store"
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
	"time"
)

var ErrForbidden = errors.New("not a room member")

type Module struct{ DB *pgxpool.Pool }
type View struct {
	Room    store.Room         `json:"room"`
	Members []store.MembersRow `json:"members"`
}

func (m *Module) View(ctx context.Context, id, actor string) (View, error) {
	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return View{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	ok, err := q.IsMember(ctx, store.IsMemberParams{RoomID: id, UserID: actor})
	if err != nil {
		return View{}, err
	}
	if !ok {
		return View{}, ErrForbidden
	}
	room, err := q.Room(ctx, id)
	if err != nil {
		return View{}, err
	}
	members, err := q.Members(ctx, id)
	if err != nil {
		return View{}, err
	}
	return View{room, members}, tx.Commit(ctx)
}
func changed(ctx context.Context, q *store.Queries, id string) error {
	if err := q.BumpRoom(ctx, id); err != nil {
		return err
	}
	return q.Enqueue(ctx, store.EnqueueParams{RoomID: id, Kind: "room.updated", Payload: []byte(`{}`)})
}
func (m *Module) List(w http.ResponseWriter, r *http.Request) {
	items, err := store.New(m.DB).MyRooms(r.Context(), httpx.Actor(r).ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}
func (m *Module) Get(w http.ResponseWriter, r *http.Request) {
	view, err := m.View(r.Context(), chi.URLParam(r, "roomID"), httpx.Actor(r).ID)
	if errors.Is(err, ErrForbidden) {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room not found")
		return
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, view)
}
func (m *Module) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Capacity int32  `json:"capacity"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 80 || in.Capacity < 2 || in.Capacity > 5 {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Name is required; capacity must be 2-5")
		return
	}
	tx, err := m.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := store.New(tx)
	room, err := q.CreateRoom(r.Context(), store.CreateRoomParams{HostID: httpx.Actor(r).ID, Name: in.Name, Capacity: in.Capacity})
	if err == nil {
		err = q.AddMember(r.Context(), store.AddMemberParams{RoomID: room.ID, UserID: httpx.Actor(r).ID, Seat: 0})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 201, room)
}
func (m *Module) Ready(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ready bool `json:"ready"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	id := chi.URLParam(r, "roomID")
	actor := httpx.Actor(r).ID
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
	ok, err := q.IsMember(r.Context(), store.IsMemberParams{RoomID: id, UserID: actor})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !ok {
		httpx.Error(w, r, 404, "NOT_FOUND", "Room not found")
		return
	}
	if room.Status != "waiting" {
		httpx.Error(w, r, 409, "ROOM_STARTED", "Room is not waiting")
		return
	}
	err = q.SetReady(r.Context(), store.SetReadyParams{RoomID: id, UserID: actor, Ready: in.Ready})
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
func (m *Module) Invite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TargetID string `json:"target_id"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	id := chi.URLParam(r, "roomID")
	actor := httpx.Actor(r).ID
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
	ok, err := q.IsMember(r.Context(), store.IsMemberParams{RoomID: id, UserID: actor})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if !ok {
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
	if int32(len(members)) >= room.Capacity {
		httpx.Error(w, r, 409, "ROOM_FULL", "Room is full")
		return
	}
	token := ""
	hash := ""
	if in.TargetID != "" {
		if !httpx.UUID(in.TargetID) {
			httpx.Error(w, r, 400, "INVALID_REQUEST", "Invalid target ID")
			return
		}
		for _, member := range members {
			if member.ID == in.TargetID {
				httpx.Error(w, r, 409, "CONFLICT", "Friend is already in this room")
				return
			}
		}
		if err = q.LockSocialPair(r.Context(), social.Pair(actor, in.TargetID)); err != nil {
			httpx.DBError(w, r, err)
			return
		}
		blocked, e := q.HasBlock(r.Context(), store.HasBlockParams{UserID: actor, BlockedID: in.TargetID})
		if e != nil {
			httpx.DBError(w, r, e)
			return
		}
		friends, e := q.AreFriends(r.Context(), store.AreFriendsParams{RequesterID: actor, RecipientID: in.TargetID})
		if e != nil {
			httpx.DBError(w, r, e)
			return
		}
		if blocked || !friends {
			httpx.Error(w, r, 403, "FORBIDDEN", "Invite an accepted friend")
			return
		}
	} else {
		token = httpx.Token()
		hash = httpx.Hash(token)
	}
	invite, err := q.CreateInvitation(r.Context(), store.CreateInvitationParams{RoomID: id, InviterID: actor, TargetID: in.TargetID, TokenHash: hash})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]any{"id": invite.ID, "expires_at": invite.ExpiresAt, "token": token})
}
func (m *Module) Invitations(w http.ResponseWriter, r *http.Request) {
	items, err := store.New(m.DB).MyInvitations(r.Context(), httpx.Actor(r).ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}
func (m *Module) Join(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token        string `json:"token"`
		InvitationID string `json:"invitation_id"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	actor := httpx.Actor(r).ID
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	var invitation store.Invitation
	if in.Token != "" && in.InvitationID == "" {
		invitation, err = q.InvitationByToken(ctx, pgtype.Text{String: httpx.Hash(in.Token), Valid: true})
	} else if httpx.UUID(in.InvitationID) && in.Token == "" {
		invitation, err = q.Invitation(ctx, in.InvitationID)
	} else {
		httpx.Error(w, r, 400, "INVALID_REQUEST", "Supply one invitation ID or link token")
		return
	}
	if err != nil {
		httpx.Error(w, r, 404, "INVITATION_INVALID", "Invitation unavailable")
		return
	}
	room, err := q.LockRoom(ctx, invitation.RoomID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	// Re-read under room lock so revocation and concurrent joins serialize.
	invitation, err = q.Invitation(ctx, invitation.ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if invitation.RevokedAt != nil || invitation.AcceptedAt != nil || !invitation.ExpiresAt.After(time.Now()) || (invitation.TargetID != nil && *invitation.TargetID != actor) {
		httpx.Error(w, r, 404, "INVITATION_INVALID", "Invitation unavailable")
		return
	}
	if room.Status != "waiting" {
		httpx.Error(w, r, 409, "ROOM_STARTED", "Room is not waiting")
		return
	}
	banned, err := q.IsRoomBanned(ctx, store.IsRoomBannedParams{RoomID: room.ID, UserID: actor})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if banned {
		httpx.Error(w, r, 403, "FORBIDDEN", "This room is unavailable")
		return
	}
	blocked, err := q.HasBlock(ctx, store.HasBlockParams{UserID: actor, BlockedID: invitation.InviterID})
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if blocked {
		httpx.Error(w, r, 403, "FORBIDDEN", "This room is unavailable")
		return
	}
	members, err := q.Members(ctx, room.ID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	for _, member := range members {
		if member.ID == actor {
			httpx.JSON(w, 200, room)
			return
		}
		blocked, e := q.HasBlock(ctx, store.HasBlockParams{UserID: actor, BlockedID: member.ID})
		if e != nil {
			httpx.DBError(w, r, e)
			return
		}
		if blocked {
			httpx.Error(w, r, 403, "FORBIDDEN", "This room is unavailable")
			return
		}
	}
	if int32(len(members)) >= room.Capacity {
		httpx.Error(w, r, 409, "ROOM_FULL", "Room is full")
		return
	}
	// A departed member may leave a gap; use the first free seat.
	used := map[int32]bool{}
	for _, m := range members {
		used[m.Seat] = true
	}
	var seat int32
	for used[seat] {
		seat++
	}
	err = q.AddMember(ctx, store.AddMemberParams{RoomID: room.ID, UserID: actor, Seat: seat})
	if err == nil {
		err = q.ResetReady(ctx, room.ID)
	}
	if err == nil {
		err = q.AcceptInvitation(ctx, invitation.ID)
	}
	if err == nil {
		err = changed(ctx, q, room.ID)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	room.Revision++
	httpx.JSON(w, 200, room)
}
func (m *Module) Revoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	inv, err := q.Invitation(ctx, chi.URLParam(r, "invitationID"))
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	room, err := q.LockRoom(ctx, inv.RoomID)
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	var n int64
	if room.HostID == httpx.Actor(r).ID {
		n, err = q.RevokeInvitationByHost(ctx, inv.ID)
	} else {
		n, err = q.RevokeInvitation(ctx, store.RevokeInvitationParams{ID: inv.ID, InviterID: httpx.Actor(r).ID})
	}
	if err != nil {
		httpx.DBError(w, r, err)
		return
	}
	if n == 0 {
		httpx.Error(w, r, 404, "NOT_FOUND", "Invitation unavailable")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		httpx.DBError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

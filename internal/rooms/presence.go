package rooms

import (
	"context"
	"errors"

	"cardplay/internal/store"
	"github.com/jackc/pgx/v5"
)

// Seen records an authenticated room subscription or heartbeat. It takes the
// room lock so an absent-host transfer cannot race a returning host.
func (m *Module) Seen(ctx context.Context, roomID, actor string) error {
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	room, err := q.LockRoom(ctx, roomID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if room.Status == "closed" {
		return ErrForbidden
	}
	n, err := q.MarkRoomSeen(ctx, store.MarkRoomSeenParams{RoomID: roomID, UserID: actor})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrForbidden
	}
	return tx.Commit(ctx)
}

// TransferAbsentHosts selects only connected candidates, then rechecks after
// locking each room. It is safe for multiple API instances to run this worker.
func (m *Module) TransferAbsentHosts(ctx context.Context) error {
	ids, err := store.New(m.DB).AbsentHostRooms(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := m.transferAbsentHost(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) transferAbsentHost(ctx context.Context, id string) error {
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	room, err := q.LockRoom(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if room.Status != "waiting" {
		return nil
	}
	absent, err := q.HostStillAbsent(ctx, store.HostStillAbsentParams{RoomID: id, UserID: room.HostID})
	if err != nil {
		return err
	}
	if !absent {
		return nil
	}
	next, err := q.OldestActiveRoomMember(ctx, store.OldestActiveRoomMemberParams{RoomID: id, UserID: room.HostID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = q.SetRoomHost(ctx, store.SetRoomHostParams{ID: id, HostID: next}); err == nil {
		err = changed(ctx, q, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

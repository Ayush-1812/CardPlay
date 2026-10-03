package trump

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"cardplay/internal/game"
)

func newMatch(t *testing.T) (game.State, *State) {
	t.Helper()
	// Seats arrive in any order; the module sorts them.
	st, err := Module{}.New(context.Background(), game.Setup{
		Participants: []game.Participant{{UserID: "u2", Seat: 2}, {UserID: "u0", Seat: 0}, {UserID: "u3", Seat: 3}, {UserID: "u1", Seat: 1}},
		Random:       bytes.NewReader(bytes.Repeat([]byte{31}, SeedSize)),
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := decode(st)
	if err != nil {
		t.Fatal(err)
	}
	return st, s
}

func TestModuleDescriptor(t *testing.T) {
	d := Module{}.Descriptor()
	if d.ID != "trump" || d.MinPlayers != Seats || d.MaxPlayers != Seats || !d.Playable {
		t.Fatalf("descriptor %+v", d)
	}
	if cards, ok := (Module{}).Cards().([]Card); !ok || len(cards) != 52 {
		t.Fatal("the card manifest must be the full deck")
	}
}

func TestModuleNewSeatsInOrder(t *testing.T) {
	_, s := newMatch(t)
	for seat, p := range s.Players {
		want := "u" + string(rune('0'+seat))
		if p.UserID != want {
			t.Fatalf("seat %d holds %s, want %s", seat, p.UserID, want)
		}
	}
	if s.Stage() != StageTrumpDecision {
		t.Fatalf("stage %s", s.Stage())
	}
	if _, err := (Module{}).New(context.Background(), game.Setup{Participants: []game.Participant{{UserID: "a", Seat: 0}}}); err == nil {
		t.Fatal("a setup without randomness must fail")
	}
}

func TestModuleApply(t *testing.T) {
	st, s := newMatch(t)
	chooser := s.Players[SeatsOfTeam(s.EntitledTeam)[0]].UserID
	cmd := func(kind, payload string) game.Command {
		return game.Command{ID: "c", Kind: kind, Payload: json.RawMessage(payload)}
	}

	t.Run("a stranger cannot act", func(t *testing.T) {
		_, err := Module{}.Apply(context.Background(), st, "nobody", cmd("choose_trump", `{"suit":"hearts"}`))
		assertRejected(t, err, CodeNotYourTurn)
	})
	t.Run("unknown commands and payloads are refused", func(t *testing.T) {
		_, err := Module{}.Apply(context.Background(), st, chooser, cmd("steal_everything", `{}`))
		assertRejected(t, err, CodeInvalidAction)
		_, err = Module{}.Apply(context.Background(), st, chooser, cmd("choose_trump", `{"suit":"hearts","bribe":5}`))
		assertRejected(t, err, CodeInvalidAction)
	})
	t.Run("the wrong team is refused", func(t *testing.T) {
		other := s.Players[SeatsOfTeam(1 - s.EntitledTeam)[0]].UserID
		_, err := Module{}.Apply(context.Background(), st, other, cmd("choose_trump", `{"suit":"hearts"}`))
		assertRejected(t, err, CodeNotYourTurn)
	})

	tr, err := Module{}.Apply(context.Background(), st, chooser, cmd("choose_trump", `{"suit":"hearts"}`))
	if err != nil {
		t.Fatal(err)
	}
	if tr.Outcome.Finished {
		t.Fatal("a trump match has rounds, never a session winner")
	}
	next, err := decode(tr.State)
	if err != nil {
		t.Fatal(err)
	}
	if next.Trump != Hearts || next.Stage() != StageActiveTrick {
		t.Fatalf("trump %s stage %s", next.Trump, next.Stage())
	}
	// Dealt cards reach only their owner; everything else is public.
	private := 0
	for _, e := range tr.Events {
		if e.Kind != "dealt_rest" {
			if !e.Public {
				t.Fatalf("%s should be public", e.Kind)
			}
			continue
		}
		private++
		if e.Public || e.AudienceUserID == "" {
			t.Fatalf("dealt cards must be private, got %+v", e)
		}
		var payload struct {
			Cards []CardID `json:"cards"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		seat := next.seatOf(e.AudienceUserID)
		for _, id := range payload.Cards {
			if !containsCard(next.Players[seat].Hand, id) {
				t.Fatalf("%s was announced to the wrong player", id)
			}
		}
	}
	if private != Seats {
		t.Fatalf("%d private deal events, want %d", private, Seats)
	}
}

func TestModuleView(t *testing.T) {
	st, s := newMatch(t)
	for seat, p := range s.Players {
		v, err := Module{}.View(st, p.UserID)
		if err != nil {
			t.Fatal(err)
		}
		var self SelfView
		if err := json.Unmarshal(v.Self, &self); err != nil {
			t.Fatal(err)
		}
		if self.Seat != seat || len(self.Hand) != FirstDeal {
			t.Fatalf("seat %d sees %+v", seat, self)
		}
		// No other player's card may appear anywhere in the projection.
		blob := string(v.Public) + string(v.Self)
		for other, op := range s.Players {
			if other == seat {
				continue
			}
			for _, id := range op.Hand {
				if containsCard(self.Hand, id) || strings.Contains(string(v.Public), string(id)) {
					t.Fatalf("seat %d can see seat %d's %s", seat, other, id)
				}
			}
		}
		if strings.Contains(blob, "seed") {
			t.Fatal("the seed must never be projected")
		}
	}
	if _, err := (Module{}).View(st, "nobody"); err == nil {
		t.Fatal("a stranger has no view")
	}
}

func TestModuleTimeoutAndChat(t *testing.T) {
	st, s := newMatch(t)
	// No automatic move is applied to a Trump match: the module must not
	// offer the platform a timeout policy (owner instruction 2026-10-03).
	if _, ok := any(Module{}).(game.TimeoutPolicy); ok {
		t.Fatal("Trump must not implement TimeoutPolicy without an approved policy")
	}
	// The engine can still compute one, for when a policy is approved.
	if _, canMove := s.TimeoutAction(SeatsOfTeam(s.EntitledTeam)[0]); !canMove {
		t.Fatal("the engine should still be able to suggest a default move")
	}
	chooser := s.Players[SeatsOfTeam(s.EntitledTeam)[0]].UserID
	tr, err := Module{}.Apply(context.Background(), st, chooser, game.Command{ID: "c", Kind: "choose_trump", Payload: json.RawMessage(`{"suit":"spades"}`)})
	if err != nil {
		t.Fatal(err)
	}
	open, err := Module{}.ChatOpen(st)
	if err != nil || open {
		t.Fatalf("chat must be closed while the trump is chosen (open=%v, err=%v)", open, err)
	}
	if open, err = (Module{}).ChatOpen(tr.State); err != nil || !open {
		t.Fatalf("chat must reopen for play (open=%v, err=%v)", open, err)
	}
}

func TestModuleValidateRejectsTampering(t *testing.T) {
	st, _ := newMatch(t)
	if err := (Module{}).Validate(st); err != nil {
		t.Fatal(err)
	}
	if err := (Module{}).Validate(game.State{SchemaVersion: SchemaVersion + 1, Data: st.Data}); err == nil {
		t.Fatal("an unknown schema must be refused")
	}
	// A state that has lost a card must not load.
	var s State
	if err := json.Unmarshal(st.Data, &s); err != nil {
		t.Fatal(err)
	}
	s.Players[0].Hand = s.Players[0].Hand[1:]
	data, err := json.Marshal(&s)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Module{}).Validate(game.State{SchemaVersion: SchemaVersion, Data: data}); err == nil {
		t.Fatal("a state missing a card must be refused")
	}
}

func assertRejected(t *testing.T, err error, code string) {
	t.Helper()
	re, ok := err.(*RuleError)
	if !ok {
		t.Fatalf("got %v, want a rule rejection", err)
	}
	if re.RejectionCode() != code {
		t.Fatalf("got %s, want %s", re.RejectionCode(), code)
	}
}

func containsCard(hand []CardID, id CardID) bool {
	for _, c := range hand {
		if c == id {
			return true
		}
	}
	return false
}

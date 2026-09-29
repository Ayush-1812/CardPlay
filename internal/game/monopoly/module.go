// Package monopoly reserves the approved edition contract without inventing a
// partially playable engine. Phase 2 will replace the fail-closed operations.
package monopoly

import (
	"cardplay/internal/game"
	"context"
)

type Module struct{}

func (Module) Descriptor() game.Descriptor {
	return game.Descriptor{ID: "monopoly-deal", Name: "Monopoly Deal", RulesVersion: "us-01723-v1", MinPlayers: 2, MaxPlayers: 5, Playable: false}
}
func (Module) New(context.Context, game.Setup) (game.State, error) {
	return game.State{}, game.ErrNotReady
}
func (Module) Validate(game.State) error { return game.ErrNotReady }
func (Module) Apply(context.Context, game.State, string, game.Command) (game.Transition, error) {
	return game.Transition{}, game.ErrNotReady
}
func (Module) View(game.State, string) (game.View, error) { return game.View{}, game.ErrNotReady }

var _ game.Game = Module{}

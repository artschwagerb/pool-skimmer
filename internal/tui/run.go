package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	endpointconfig "pool-skimmer/internal/config"
	"pool-skimmer/internal/scim"
)

// Run starts the full-screen interactive application and blocks until it exits.
func Run(ctx context.Context, client *scim.Client, endpointName, endpoint, version string) error {
	model := NewModel(ctx, client, endpoint, version)
	model.endpointName = endpointName
	program := tea.NewProgram(model)
	_, err := program.Run()
	return err
}

// RunEndpointSelector starts the TUI at the named endpoint selector.
func RunEndpointSelector(ctx context.Context, store *endpointconfig.Store, endpoints []endpointconfig.Endpoint, version string) error {
	program := tea.NewProgram(NewEndpointModel(ctx, store, endpoints, version))
	_, err := program.Run()
	return err
}

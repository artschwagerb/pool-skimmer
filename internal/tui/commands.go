package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"pool-skimmer/internal/exportfile"
	"pool-skimmer/internal/scim"
)

func loadResourcesCmd(ctx context.Context, client *scim.Client, resourceType string) tea.Cmd {
	return func() tea.Msg {
		resources, err := client.ListAll(ctx, resourceType, scim.ListOptions{Count: 100})
		return resourcesLoadedMsg{resourceType: resourceType, resources: resources, err: err}
	}
}

func loadGroupCmd(ctx context.Context, client *scim.Client, id string, returnTo screen) tea.Cmd {
	return func() tea.Msg {
		group, err := client.Get(ctx, "Groups", id, []string{"id", "displayName", "members", "meta"}, nil)
		return groupLoadedMsg{group: group, returnTo: returnTo, err: err}
	}
}

func exportResourcesCmd(path, resourceType string, resources []scim.Resource) tea.Cmd {
	return func() tea.Msg {
		err := exportfile.WriteJSON(path, resources, false)
		return exportFinishedMsg{resourceType: resourceType, path: path, count: len(resources), err: err}
	}
}

func patchAndGetCmd(
	ctx context.Context,
	client *scim.Client,
	resourceType, id string,
	ifMatch string,
	operations []any,
	status string,
	next, failure screen,
) tea.Cmd {
	return func() tea.Msg {
		document := map[string]any{
			"schemas":    []string{scim.PatchOpSchema},
			"Operations": operations,
		}
		if _, err := client.Patch(ctx, resourceType, id, document, ifMatch); err != nil {
			return mutationFinishedMsg{resourceType: resourceType, id: id, status: status, next: next, failure: failure, err: err}
		}
		resource, err := client.Get(ctx, resourceType, id, nil, nil)
		if err != nil {
			err = &verificationError{cause: err}
		}
		return mutationFinishedMsg{
			resourceType: resourceType, id: id, resource: resource,
			status: status, next: next, failure: failure, err: err,
		}
	}
}

func addMemberCmd(ctx context.Context, client *scim.Client, groupID, userID, ifMatch string) tea.Cmd {
	operations := []any{map[string]any{
		"op": "add", "path": "members", "value": []any{map[string]any{"value": userID}},
	}}
	return patchAndGetCmd(ctx, client, "Groups", groupID, ifMatch, operations, "Group member added", screenMembers, screenMemberAdd)
}

func deleteCmd(ctx context.Context, client *scim.Client, resourceType, id, ifMatch string) tea.Cmd {
	return func() tea.Msg {
		err := client.Delete(ctx, resourceType, id, ifMatch)
		return mutationFinishedMsg{
			resourceType: resourceType, id: id, deleted: err == nil,
			status: "Deleted " + singular(resourceType), next: screenBrowse, failure: screenConfirm, err: err,
		}
	}
}

type verificationError struct {
	cause error
}

func (e *verificationError) Error() string {
	return "update succeeded but read-back verification failed: " + e.cause.Error()
}

func (e *verificationError) Unwrap() error {
	return e.cause
}

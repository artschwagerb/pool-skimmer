package tui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	endpointconfig "pool-skimmer/internal/config"
	"pool-skimmer/internal/scim"
)

func TestEndpointSelectorRendersNamedEndpointsAndCreateOption(t *testing.T) {
	store := endpointconfig.NewStore(filepath.Join(t.TempDir(), endpointconfig.DirectoryName))
	model := NewEndpointModel(context.Background(), store, []endpointconfig.Endpoint{
		{ID: "one", Name: "Production", URL: "https://example.test/scim/v2", AuthHeader: "Authorization", AuthScheme: "Bearer", Timeout: "30s", ReadRetries: 2},
		{ID: "two", Name: "Staging", URL: "https://staging.example.test/scim/v2", AuthHeader: "Authorization", AuthScheme: "Bearer", Timeout: "30s", ReadRetries: 2},
	}, "test")
	view := ansi.Strip(model.View().Content)
	for _, expected := range []string{"Select a SCIM Endpoint or Create an Endpoint", "Production", "Staging", "+ Create an Endpoint"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestEndpointFormMasksAPIKey(t *testing.T) {
	store := endpointconfig.NewStore(filepath.Join(t.TempDir(), endpointconfig.DirectoryName))
	model := NewEndpointModel(context.Background(), store, nil, "test")
	updated, _ := model.startEndpointForm(endpointconfig.Endpoint{})
	model = updated.(Model)
	model.resize(80, 24)
	model.endpointInputs[endpointAPIKeyInput].SetValue("never-render-this-secret")
	view := model.View().Content
	if strings.Contains(view, "never-render-this-secret") {
		t.Fatal("endpoint form rendered the API key")
	}
	plain := ansi.Strip(view)
	if lines := strings.Count(plain, "\n") + 1; lines > 24 {
		t.Fatalf("endpoint form rendered %d lines in a 24-line terminal:\n%s", lines, plain)
	}
	if !strings.Contains(plain, "ctrl+s save") {
		t.Fatalf("endpoint form footer is not visible:\n%s", plain)
	}
}

func TestPasteIsForwardedToFocusedMaskedAPIKeyInput(t *testing.T) {
	store := endpointconfig.NewStore(filepath.Join(t.TempDir(), endpointconfig.DirectoryName))
	model := NewEndpointModel(context.Background(), store, nil, "test")
	updated, _ := model.startEndpointForm(endpointconfig.Endpoint{})
	model = updated.(Model)
	model.endpointInputs[model.endpointInputIndex].Blur()
	model.endpointInputIndex = endpointAPIKeyInput
	model.endpointInputs[endpointAPIKeyInput].Focus()

	updated, _ = model.handlePaste(tea.PasteMsg{Content: "pasted-secret"})
	model = updated.(Model)
	if got := model.endpointInputs[endpointAPIKeyInput].Value(); got != "pasted-secret" {
		t.Fatalf("pasted API key = %q", got)
	}
	if strings.Contains(model.View().Content, "pasted-secret") {
		t.Fatal("endpoint form rendered the pasted API key")
	}
}

func TestPasteIsForwardedToOtherFocusedInputs(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")

	model.screen = screenFilter
	model.filterInput.Focus()
	updated, _ := model.handlePaste(tea.PasteMsg{Content: "alice@example.com"})
	model = updated.(Model)
	if got := model.filterInput.Value(); got != "alice@example.com" {
		t.Fatalf("pasted filter = %q", got)
	}

	model.screen = screenMemberAdd
	model.memberInput.Focus()
	updated, _ = model.handlePaste(tea.PasteMsg{Content: "user-123"})
	model = updated.(Model)
	if got := model.memberInput.Value(); got != "user-123" {
		t.Fatalf("pasted member = %q", got)
	}
}

func TestEndpointSelectorConnectsWithSavedCredential(t *testing.T) {
	store := endpointconfig.NewStore(filepath.Join(t.TempDir(), endpointconfig.DirectoryName))
	endpoint, err := store.Save(endpointconfig.Endpoint{
		Name: "Production", URL: "https://example.test/scim/v2", AuthHeader: "Authorization", AuthScheme: "Bearer",
		Timeout: "30s", ReadRetries: 2,
	}, "secret")
	if err != nil {
		t.Fatal(err)
	}
	model := NewEndpointModel(context.Background(), store, []endpointconfig.Endpoint{endpoint}, "test")
	updated, command := model.updateEndpointSelect(keyPress(tea.KeyEnter, ""))
	model = updated.(Model)
	if command == nil || !model.busy {
		t.Fatalf("command=%v busy=%v", command, model.busy)
	}
	batch := command().(tea.BatchMsg)
	var connected endpointConnectedMsg
	for _, child := range batch {
		if message, ok := child().(endpointConnectedMsg); ok {
			connected = message
		}
	}
	if connected.err != nil || connected.client == nil {
		t.Fatalf("connected = %#v", connected)
	}
	updated, command = model.handleEndpointConnected(connected)
	model = updated.(Model)
	if model.screen != screenBrowse || model.endpointName != "Production" || command == nil || model.pendingLoad != 2 {
		t.Fatalf("screen=%v name=%q command=%v pending=%d", model.screen, model.endpointName, command, model.pendingLoad)
	}
}

func TestBrowserRendersLoadedResources(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test/scim/v2", "test")
	model.pendingLoad = 0
	model.setResources("Users", []scim.Resource{
		{"id": "u-1", "userName": "alice@example.com", "displayName": "Alice Example", "active": true},
	})
	model.setResources("Groups", []scim.Resource{
		{"id": "g-1", "displayName": "Platform", "members": []any{}},
	})
	model.applyFilter("")
	model.resize(120, 32)

	view := model.View().Content
	for _, expected := range []string{"POOL SKIMMER", "Alice Example", "alice@example.com", "Enabled"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestDetailCopyCopiesOnlyResourceJSON(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.pendingLoad = 0
	model.setResources("Users", []scim.Resource{
		{"id": "u-1", "userName": "alice@example.com", "displayName": "Alice Example", "active": true},
	})
	model.applyFilter("")
	model.screen = screenDetail
	model.refreshDetail()

	var copied string
	model.setClipboard = func(content string) tea.Cmd {
		copied = content
		return func() tea.Msg { return nil }
	}
	updated, command := model.updateDetail(keyPress('c', "c"))
	model = updated.(Model)

	want, err := resourceJSON(model.visible[0])
	if err != nil {
		t.Fatal(err)
	}
	if copied != want {
		t.Fatalf("copied content = %q, want %q", copied, want)
	}
	if command == nil {
		t.Fatal("copy did not return a clipboard command")
	}
	for _, unwanted := range []string{"POOL SKIMMER", "╭", "╰", "copy JSON"} {
		if strings.Contains(copied, unwanted) {
			t.Fatalf("copied content contains rendered TUI text %q: %q", unwanted, copied)
		}
	}
	if model.status != "Copied resource JSON to clipboard" || model.errorText != "" {
		t.Fatalf("status=%q error=%q", model.status, model.errorText)
	}
}

func TestBrowserExportWritesCurrentlyShownResources(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.pendingLoad = 0
	model.setResources("Users", []scim.Resource{
		{"id": "u-1", "userName": "alice@example.test", "active": true},
		{"id": "u-2", "userName": "bob@example.test", "active": false},
	})
	model.userFilter = "enabled=true"
	model.applyFilter("")

	updated, _ := model.updateBrowse(keyPress('x', "x"))
	model = updated.(Model)
	if model.screen != screenExport || model.exportInput.Value() != "users.json" {
		t.Fatalf("screen=%v path=%q", model.screen, model.exportInput.Value())
	}
	plain := ansi.Strip(model.View().Content)
	if !strings.Contains(plain, "Export users") || !strings.Contains(plain, "1 currently shown users") {
		t.Fatalf("export view is missing context:\n%s", plain)
	}

	path := filepath.Join(t.TempDir(), "enabled-users.json")
	model.exportInput.SetValue(path)
	updated, command := model.updateExport(keyPress(tea.KeyEnter, ""))
	model = updated.(Model)
	if command == nil || !model.busy {
		t.Fatalf("command=%v busy=%v", command, model.busy)
	}
	message := exportMessage(t, command)
	updated, _ = model.handleExportFinished(message)
	model = updated.(Model)
	if model.screen != screenBrowse || model.errorText != "" || !strings.Contains(model.status, "Exported 1 users") {
		t.Fatalf("screen=%v status=%q error=%q", model.screen, model.status, model.errorText)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var resources []scim.Resource
	if err := json.Unmarshal(data, &resources); err != nil {
		t.Fatal(err)
	}
	if got := resourceIDs(resources); !equalStrings(got, []string{"u-1"}) {
		t.Fatalf("exported IDs = %v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("export permissions = %o", info.Mode().Perm())
	}
}

func TestBrowserExportSupportsGroupsAndProtectsExistingFiles(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.pendingLoad = 0
	model.activeType = "Groups"
	model.setResources("Groups", []scim.Resource{{"id": "g-1", "displayName": "Platform"}})
	model.applyFilter("")

	updated, _ := model.updateBrowse(keyPress('x', "x"))
	model = updated.(Model)
	if model.screen != screenExport || model.exportInput.Value() != "groups.json" {
		t.Fatalf("screen=%v path=%q", model.screen, model.exportInput.Value())
	}

	path := filepath.Join(t.TempDir(), "groups.json")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	model.exportInput.SetValue(path)
	updated, command := model.updateExport(keyPress(tea.KeyEnter, ""))
	model = updated.(Model)
	message := exportMessage(t, command)
	updated, command = model.handleExportFinished(message)
	model = updated.(Model)
	if model.screen != screenExport || command == nil || !strings.Contains(model.errorText, "already exists") {
		t.Fatalf("screen=%v command=%v error=%q", model.screen, command, model.errorText)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep me" {
		t.Fatalf("file=%q error=%v", data, err)
	}
}

func exportMessage(t *testing.T, command tea.Cmd) exportFinishedMsg {
	t.Helper()
	batch, ok := command().(tea.BatchMsg)
	if !ok {
		t.Fatalf("command did not return a batch")
	}
	for _, child := range batch {
		if child == nil {
			continue
		}
		if message, ok := child().(exportFinishedMsg); ok {
			return message
		}
	}
	t.Fatal("batch did not contain an export result")
	return exportFinishedMsg{}
}

func TestLocalFilterMatchesNamesUsernamesAndIDs(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.setResources("Users", []scim.Resource{
		{"id": "user-1", "userName": "alice@example.com", "displayName": "Alice"},
		{"id": "user-2", "userName": "bob@example.com", "displayName": "Bob"},
	})

	model.userFilter = "BOB@EXAMPLE"
	model.applyFilter("")
	if len(model.visible) != 1 || stringValue(model.visible[0]["id"]) != "user-2" {
		t.Fatalf("visible = %#v", model.visible)
	}

	model.userFilter = "user-1"
	model.applyFilter("")
	if len(model.visible) != 1 || stringValue(model.visible[0]["userName"]) != "alice@example.com" {
		t.Fatalf("visible = %#v", model.visible)
	}
}

func TestStructuredUserFiltersSupportAliasesQuotesAndAnd(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.setResources("Users", []scim.Resource{
		{"id": "u-1", "userName": "alice@example.com", "displayName": "Alice Example", "active": true, "name": map[string]any{"givenName": "Alice", "familyName": "Example"}},
		{"id": "u-2", "userName": "bob@example.com", "displayName": "Bob Example", "active": false},
		{"id": "u-3", "userName": "carol@other.test", "displayName": "Carol Else", "active": true},
	})

	tests := []struct {
		filter string
		want   []string
	}{
		{filter: "enabled=true", want: []string{"u-1", "u-3"}},
		{filter: "status=disabled", want: []string{"u-2"}},
		{filter: "username~@example.com enabled=true", want: []string{"u-1"}},
		{filter: `name="Alice Example"`, want: []string{"u-1"}},
		{filter: "givenName=alice", want: []string{"u-1"}},
	}
	for _, test := range tests {
		model.userFilter = test.filter
		model.applyFilter("")
		if got := resourceIDs(model.visible); !equalStrings(got, test.want) {
			t.Errorf("filter %q IDs = %v, want %v", test.filter, got, test.want)
		}
	}
}

func TestStructuredGroupFilterSupportsNumericMemberComparisons(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.activeType = "Groups"
	model.setResources("Groups", []scim.Resource{
		{"id": "g-0", "displayName": "Empty", "members": []any{}},
		{"id": "g-2", "displayName": "Pair", "members": []any{map[string]any{"value": "u-1"}, map[string]any{"value": "u-2"}}},
		{"id": "g-1", "displayName": "Solo", "members": []any{map[string]any{"value": "u-1"}}},
	})
	model.groupFilter = "members>0"
	model.applyFilter("")

	if got, want := resourceIDs(model.visible), []string{"g-2", "g-1"}; !equalStrings(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
}

func TestInvalidFilterKeepsLastValidResults(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.setResources("Users", []scim.Resource{
		{"id": "u-1", "displayName": "Alice", "active": true},
		{"id": "u-2", "displayName": "Bob", "active": false},
	})
	model.userFilter = "enabled=true"
	model.applyFilter("")
	model.screen = screenFilter
	model.filterInput.SetValue("enabled=maybe")

	updated, command := model.updateFilter(keyPress(tea.KeyEnter, ""))
	model = updated.(Model)
	if command != nil || model.screen != screenFilter {
		t.Fatalf("invalid filter command=%v screen=%v", command, model.screen)
	}
	if model.userFilter != "enabled=true" || !equalStrings(resourceIDs(model.visible), []string{"u-1"}) {
		t.Fatalf("invalid filter changed state: filter=%q IDs=%v", model.userFilter, resourceIDs(model.visible))
	}
	if !strings.Contains(model.errorText, "true or false") {
		t.Fatalf("error = %q", model.errorText)
	}

	model.filterInput.SetValue("status=disabled")
	updated, _ = model.updateFilter(keyPress(tea.KeyEnter, ""))
	model = updated.(Model)
	if model.screen != screenBrowse || model.userFilter != "status=disabled" || model.errorText != "" {
		t.Fatalf("valid filter state: screen=%v filter=%q error=%q", model.screen, model.userFilter, model.errorText)
	}
	if got := resourceIDs(model.visible); !equalStrings(got, []string{"u-2"}) {
		t.Fatalf("valid filter IDs = %v", got)
	}
}

func TestSortPickerAppliesDirectionAndPreservesSelection(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.setResources("Users", []scim.Resource{
		{"id": "u-1", "userName": "zeta@example.com", "displayName": "Alice", "active": true},
		{"id": "u-2", "userName": "alpha@example.com", "displayName": "Bob", "active": false},
		{"id": "u-3", "userName": "mike@example.com", "displayName": "Carol", "active": true},
	})
	model.applyFilter("")
	model.table.SetCursor(1)

	updated, _ := model.startSort()
	model = updated.(Model)
	if model.screen != screenSort {
		t.Fatalf("screen = %v, want sort", model.screen)
	}
	plain := ansi.Strip(model.View().Content)
	if !strings.Contains(plain, "Sort users") || !strings.Contains(plain, "Direction: Ascending") {
		t.Fatalf("sort view is missing controls:\n%s", plain)
	}
	updated, _ = model.updateSort(keyPress(tea.KeyDown, ""))
	model = updated.(Model)
	updated, _ = model.updateSort(keyPress(tea.KeyRight, ""))
	model = updated.(Model)
	updated, _ = model.updateSort(keyPress(tea.KeyEnter, ""))
	model = updated.(Model)

	if model.screen != screenBrowse || model.userSort != (sortSpec{key: "username", descending: true}) {
		t.Fatalf("screen=%v sort=%#v", model.screen, model.userSort)
	}
	if got, want := resourceIDs(model.visible), []string{"u-1", "u-3", "u-2"}; !equalStrings(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
	if selected := model.selectedResource(); selected == nil || stringValue((*selected)["id"]) != "u-2" {
		t.Fatalf("selection = %#v, want u-2", selected)
	}
	if title := model.table.Columns()[1].Title; title != "USERNAME ↓" {
		t.Fatalf("username column title = %q", title)
	}
}

func TestFiltersAndSortsPersistIndependentlyPerResourceType(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.setResources("Users", []scim.Resource{
		{"id": "u-1", "userName": "zeta", "displayName": "Alice", "active": true},
		{"id": "u-2", "userName": "alpha", "displayName": "Bob", "active": false},
	})
	model.setResources("Groups", []scim.Resource{
		{"id": "g-1", "displayName": "One", "members": []any{map[string]any{"value": "u-1"}}},
		{"id": "g-2", "displayName": "Two", "members": []any{}},
	})
	model.userFilter = "enabled=true"
	model.userSort = sortSpec{key: "username", descending: true}
	model.applyFilter("")

	model.switchResourceType()
	model.groupFilter = "members>0"
	model.groupSort = sortSpec{key: "members", descending: true}
	model.applyFilter("")
	if got := resourceIDs(model.visible); !equalStrings(got, []string{"g-1"}) {
		t.Fatalf("group IDs = %v", got)
	}

	model.switchResourceType()
	if model.currentFilter() != "enabled=true" || model.currentSort() != (sortSpec{key: "username", descending: true}) {
		t.Fatalf("user controls: filter=%q sort=%#v", model.currentFilter(), model.currentSort())
	}
	if got := resourceIDs(model.visible); !equalStrings(got, []string{"u-1"}) {
		t.Fatalf("user IDs = %v", got)
	}
}

func TestSortUsesTypedStatusAndMemberCounts(t *testing.T) {
	users := []scim.Resource{
		{"id": "u-disabled", "displayName": "A", "active": false},
		{"id": "u-enabled", "displayName": "Z", "active": true},
	}
	sortVisibleResources(users, "Users", sortSpec{key: "status", descending: true})
	if got := resourceIDs(users); !equalStrings(got, []string{"u-enabled", "u-disabled"}) {
		t.Fatalf("status sort IDs = %v", got)
	}

	groups := []scim.Resource{
		{"id": "g-2", "members": make([]any, 2)},
		{"id": "g-10", "members": make([]any, 10)},
	}
	sortVisibleResources(groups, "Groups", sortSpec{key: "members", descending: true})
	if got := resourceIDs(groups); !equalStrings(got, []string{"g-10", "g-2"}) {
		t.Fatalf("member sort IDs = %v", got)
	}
}

func TestEditOperationsOnlyIncludesChangesAndTypesActive(t *testing.T) {
	fields := []editField{
		{label: "Display name", path: "displayName", original: "Alice"},
		{label: "Active", path: "active", original: "true"},
	}
	inputs := []textinput.Model{newInput(""), newInput("")}
	inputs[0].SetValue("Alice Example")
	inputs[1].SetValue("false")

	operations, err := editOperations(fields, inputs)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(operations)
	if !strings.Contains(string(encoded), `"value":"Alice Example"`) || !strings.Contains(string(encoded), `"value":false`) {
		t.Fatalf("operations = %s", encoded)
	}
}

func TestEditOperationsRejectsInvalidActiveValue(t *testing.T) {
	input := newInput("")
	input.SetValue("maybe")
	_, err := editOperations([]editField{{label: "Active", path: "active", original: "true"}}, []textinput.Model{input})
	if err == nil || !strings.Contains(err.Error(), "true or false") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfirmationRequiresExplicitY(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.screen = screenConfirm
	model.confirm = pendingAction{kind: actionDelete, returnTo: screenBrowse}

	updated, command := model.updateConfirm(keyPress(tea.KeyEnter, ""))
	result := updated.(Model)
	if result.busy || command != nil || result.screen != screenConfirm {
		t.Fatalf("enter unexpectedly confirmed: busy=%v command=%v screen=%v", result.busy, command, result.screen)
	}
}

func TestMutationReturnsToBrowserWhenResourceNoLongerMatchesFilter(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.setResources("Users", []scim.Resource{
		{"id": "u-1", "displayName": "Alice", "active": true},
		{"id": "u-2", "displayName": "Bob", "active": true},
	})
	model.userFilter = "enabled=true"
	model.applyFilter("")
	model.screen = screenDetail

	updated, command := model.handleMutation(mutationFinishedMsg{
		resourceType: "Users",
		id:           "u-1",
		resource:     scim.Resource{"id": "u-1", "displayName": "Alice", "active": false},
		status:       "User deactivated",
		next:         screenDetail,
		failure:      screenDetail,
	})
	model = updated.(Model)
	if command != nil || model.screen != screenBrowse {
		t.Fatalf("command=%v screen=%v", command, model.screen)
	}
	if got := resourceIDs(model.visible); !equalStrings(got, []string{"u-2"}) {
		t.Fatalf("visible IDs = %v", got)
	}
}

func TestRefreshReloadsUsersAndGroups(t *testing.T) {
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests[request.URL.Path]++
		writer.Header().Set("Content-Type", "application/scim+json")
		switch request.URL.Path {
		case "/Users":
			io.WriteString(writer, `{"Resources":[{"id":"u-new","userName":"new@example.com"}],"startIndex":1,"itemsPerPage":1,"totalResults":1}`)
		case "/Groups":
			io.WriteString(writer, `{"Resources":[{"id":"g-new","displayName":"New Group"}],"startIndex":1,"itemsPerPage":1,"totalResults":1}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := scim.NewClient(server.URL, "secret", "Authorization", "Bearer", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(context.Background(), client, server.URL, "test")
	model.pendingLoad = 0
	model.setResources("Users", []scim.Resource{{"id": "u-old", "userName": "old@example.com"}})
	model.setResources("Groups", []scim.Resource{{"id": "g-old", "displayName": "Old Group"}})
	model.applyFilter("")

	updated, command := model.updateBrowse(keyPress('r', "r"))
	model = updated.(Model)
	if command == nil || !model.busy || model.pendingLoad != 2 || model.status != "Refreshing users and groups…" {
		t.Fatalf("refresh state: command=%v busy=%v pending=%d status=%q", command, model.busy, model.pendingLoad, model.status)
	}

	batch, ok := command().(tea.BatchMsg)
	if !ok {
		t.Fatalf("refresh command returned %T, want tea.BatchMsg", command())
	}
	for _, child := range batch {
		message := child()
		if loaded, ok := message.(resourcesLoadedMsg); ok {
			updated, _ = model.handleResourcesLoaded(loaded)
			model = updated.(Model)
		}
	}

	if requests["/Users"] != 1 || requests["/Groups"] != 1 {
		t.Fatalf("requests = %#v", requests)
	}
	if model.busy || model.pendingLoad != 0 {
		t.Fatalf("refresh did not finish: busy=%v pending=%d", model.busy, model.pendingLoad)
	}
	if len(model.users) != 1 || stringValue(model.users[0]["id"]) != "u-new" {
		t.Fatalf("users = %#v", model.users)
	}
	if len(model.groups) != 1 || stringValue(model.groups[0]["id"]) != "g-new" {
		t.Fatalf("groups = %#v", model.groups)
	}
}

func TestHelpFromMembersReturnsToOriginalScreen(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.screen = screenMembers
	model.returnTo = screenDetail

	updated, _ := model.updateMembers(keyPress('?', "?"))
	model = updated.(Model)
	if model.screen != screenHelp || model.returnTo != screenDetail {
		t.Fatalf("opening help changed member return: screen=%v return=%v", model.screen, model.returnTo)
	}
	updated, _ = model.handleKey(keyPress('?', "?"))
	model = updated.(Model)
	updated, _ = model.updateMembers(keyPress(tea.KeyEscape, ""))
	model = updated.(Model)
	if model.screen != screenDetail {
		t.Fatalf("screen = %v, want detail", model.screen)
	}
}

func TestPatchAndGetUsesVersionAndReturnsVerifiedResource(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		switch requests {
		case 1:
			if request.Method != http.MethodPatch || request.Header.Get("If-Match") != `W/"one"` {
				t.Fatalf("patch = %s If-Match %q", request.Method, request.Header.Get("If-Match"))
			}
			writer.WriteHeader(http.StatusNoContent)
		case 2:
			if request.Method != http.MethodGet {
				t.Fatalf("method = %s", request.Method)
			}
			io.WriteString(writer, `{"id":"u-1","displayName":"Alice Example","active":true}`)
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client, err := scim.NewClient(server.URL, "secret", "Authorization", "Bearer", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	command := patchAndGetCmd(
		context.Background(), client, "Users", "u-1", `W/"one"`,
		[]any{map[string]any{"op": "replace", "path": "displayName", "value": "Alice Example"}},
		"Saved changes", screenDetail, screenEdit,
	)
	message := command().(mutationFinishedMsg)
	if message.err != nil || requests != 2 || stringValue(message.resource["displayName"]) != "Alice Example" {
		t.Fatalf("message=%#v requests=%d", message, requests)
	}
}

func TestResolveUserIDAcceptsUsernameOrID(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.users = []scim.Resource{{"id": "u-1", "userName": "Alice@Example.com"}}
	if got := model.resolveUserID("alice@example.com"); got != "u-1" {
		t.Fatalf("username resolved to %q", got)
	}
	if got := model.resolveUserID("external-id"); got != "external-id" {
		t.Fatalf("raw ID resolved to %q", got)
	}
}

func TestOpeningPopulatedGroupInitializesMemberColumnsBeforeRows(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.setResources("Groups", []scim.Resource{{"id": "g-1", "displayName": "Platform"}})
	group := scim.Resource{
		"id": "g-1", "displayName": "Platform",
		"members": []any{map[string]any{"value": "u-1", "display": "Alice", "type": "User"}},
	}

	updated, command := model.handleGroupLoaded(groupLoadedMsg{group: group, returnTo: screenDetail})
	result := updated.(Model)
	if command != nil || result.screen != screenMembers || len(result.memberTable.Rows()) != 1 || len(result.memberTable.Columns()) != 3 {
		t.Fatalf("screen=%v rows=%d columns=%d command=%v", result.screen, len(result.memberTable.Rows()), len(result.memberTable.Columns()), command)
	}
}

func TestStyledFooterAndStatusUseANSIVisibleWidth(t *testing.T) {
	model := NewModel(context.Background(), nil, "https://example.test", "test")
	model.width = 80
	model.screen = screenDetail
	footer := ansi.Strip(model.renderFooter())
	status := ansi.Strip(model.renderStatus())
	if !strings.Contains(footer, "esc back") || strings.Contains(footer, "[1;") {
		t.Fatalf("footer = %q", footer)
	}
	if !strings.Contains(footer, "c copy JSON") {
		t.Fatalf("detail footer does not advertise copy: %q", footer)
	}
	if !strings.Contains(status, "Connecting to the SCIM endpoint") || strings.Contains(status, "[1;") {
		t.Fatalf("status = %q", status)
	}
}

func keyPress(code rune, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Text: text})
}

func resourceIDs(resources []scim.Resource) []string {
	ids := make([]string, 0, len(resources))
	for _, resource := range resources {
		ids = append(ids, stringValue(resource["id"]))
	}
	return ids
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

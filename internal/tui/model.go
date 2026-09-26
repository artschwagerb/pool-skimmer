// Package tui implements the interactive pool-skimmer terminal interface.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	endpointconfig "pool-skimmer/internal/config"
	"pool-skimmer/internal/scim"
)

type screen uint8

const (
	screenEndpointSelect screen = iota
	screenEndpointCreate
	screenBrowse
	screenDetail
	screenFilter
	screenSort
	screenExport
	screenEdit
	screenConfirm
	screenMembers
	screenMemberAdd
	screenHelp
)

type actionKind uint8

const (
	actionNone actionKind = iota
	actionDelete
	actionToggleActive
	actionRemoveMember
)

type editField struct {
	label    string
	path     string
	original string
}

type pendingAction struct {
	kind         actionKind
	resourceType string
	id           string
	label        string
	memberID     string
	active       bool
	ifMatch      string
	returnTo     screen
}

type resourcesLoadedMsg struct {
	resourceType string
	resources    []scim.Resource
	err          error
}

type groupLoadedMsg struct {
	group    scim.Resource
	returnTo screen
	err      error
}

type mutationFinishedMsg struct {
	resourceType string
	id           string
	resource     scim.Resource
	deleted      bool
	status       string
	next         screen
	failure      screen
	err          error
}

type exportFinishedMsg struct {
	resourceType string
	path         string
	count        int
	err          error
}

// Model is the complete interactive application state.
type Model struct {
	ctx                context.Context
	client             *scim.Client
	endpointName       string
	endpoint           string
	version            string
	endpointStore      *endpointconfig.Store
	endpoints          []endpointconfig.Endpoint
	endpointIndex      int
	endpointInputs     []textinput.Model
	endpointInputIndex int
	editingEndpointID  string
	endpointReturn     screen

	width  int
	height int
	screen screen

	activeType     string
	users          []scim.Resource
	groups         []scim.Resource
	visible        []scim.Resource
	userFilter     string
	groupFilter    string
	userSort       sortSpec
	groupSort      sortSpec
	sortIndex      int
	sortDescending bool

	table       table.Model
	members     []scim.Resource
	memberTable table.Model
	memberGroup string
	returnTo    screen
	helpReturn  screen
	refreshID   string
	detail      viewport.Model

	filterInput textinput.Model
	exportInput textinput.Model
	memberInput textinput.Model
	editFields  []editField
	editInputs  []textinput.Model
	editIndex   int

	spinner      spinner.Model
	pendingLoad  int
	busy         bool
	status       string
	errorText    string
	confirm      pendingAction
	setClipboard func(string) tea.Cmd
}

// NewModel creates a TUI model. It does not contact the endpoint until Init runs.
func NewModel(ctx context.Context, client *scim.Client, endpoint, version string) Model {
	model := newBaseModel(ctx, version)
	model.client = client
	model.endpoint = endpoint
	model.screen = screenBrowse
	model.pendingLoad = 2
	model.status = "Connecting to the SCIM endpoint…"
	return model
}

// NewEndpointModel creates a TUI that begins with named endpoint selection.
func NewEndpointModel(ctx context.Context, store *endpointconfig.Store, endpoints []endpointconfig.Endpoint, version string) Model {
	model := newBaseModel(ctx, version)
	model.endpointStore = store
	model.endpoints = append([]endpointconfig.Endpoint(nil), endpoints...)
	model.screen = screenEndpointSelect
	model.status = fmt.Sprintf("Configuration: %s", store.ConfigPath())
	return model
}

func newBaseModel(ctx context.Context, version string) Model {
	resourceTable := table.New(
		table.WithFocused(true),
		table.WithHeight(12),
		table.WithWidth(80),
	)
	resourceTable.SetStyles(tableStyles())

	memberTable := table.New(
		table.WithFocused(true),
		table.WithHeight(12),
		table.WithWidth(80),
	)
	memberTable.SetStyles(tableStyles())

	filterInput := newInput("Search loaded resources…")
	exportInput := newInput("Export file path")
	memberInput := newInput("User ID or exact username")

	return Model{
		ctx:          ctx,
		version:      version,
		width:        100,
		height:       30,
		screen:       screenEndpointSelect,
		activeType:   "Users",
		userSort:     defaultSort("Users"),
		groupSort:    defaultSort("Groups"),
		table:        resourceTable,
		memberTable:  memberTable,
		detail:       viewport.New(viewport.WithWidth(40), viewport.WithHeight(16)),
		filterInput:  filterInput,
		exportInput:  exportInput,
		memberInput:  memberInput,
		spinner:      spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(accentStyle)),
		setClipboard: tea.SetClipboard,
	}
}

// Init loads both resource types in parallel.
func (m Model) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return tea.Batch(
		m.spinner.Tick,
		loadResourcesCmd(m.ctx, m.client, "Users"),
		loadResourcesCmd(m.ctx, m.client, "Groups"),
	)
}

// Update applies terminal and network events.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case spinner.TickMsg:
		var command tea.Cmd
		m.spinner, command = m.spinner.Update(msg)
		if m.pendingLoad > 0 || m.busy {
			return m, command
		}
		return m, nil
	case resourcesLoadedMsg:
		return m.handleResourcesLoaded(msg)
	case groupLoadedMsg:
		return m.handleGroupLoaded(msg)
	case mutationFinishedMsg:
		return m.handleMutation(msg)
	case exportFinishedMsg:
		return m.handleExportFinished(msg)
	case endpointConnectedMsg:
		return m.handleEndpointConnected(msg)
	case tea.PasteMsg:
		return m.handlePaste(msg)
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handlePaste(message tea.PasteMsg) (tea.Model, tea.Cmd) {
	var command tea.Cmd
	switch m.screen {
	case screenEndpointCreate:
		m.endpointInputs[m.endpointInputIndex], command = m.endpointInputs[m.endpointInputIndex].Update(message)
	case screenFilter:
		m.filterInput, command = m.filterInput.Update(message)
	case screenExport:
		m.exportInput, command = m.exportInput.Update(message)
	case screenEdit:
		m.editInputs[m.editIndex], command = m.editInputs[m.editIndex].Update(message)
	case screenMemberAdd:
		m.memberInput, command = m.memberInput.Update(message)
	}
	return m, command
}

func (m Model) handleKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenEndpointSelect:
		return m.updateEndpointSelect(key)
	case screenEndpointCreate:
		return m.updateEndpointCreate(key)
	case screenBrowse:
		return m.updateBrowse(key)
	case screenDetail:
		return m.updateDetail(key)
	case screenFilter:
		return m.updateFilter(key)
	case screenSort:
		return m.updateSort(key)
	case screenExport:
		return m.updateExport(key)
	case screenEdit:
		return m.updateEdit(key)
	case screenConfirm:
		return m.updateConfirm(key)
	case screenMembers:
		return m.updateMembers(key)
	case screenMemberAdd:
		return m.updateMemberAdd(key)
	case screenHelp:
		if key.String() == "?" || key.String() == "esc" || key.String() == "q" {
			m.screen = m.helpReturn
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) updateBrowse(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "tab", "right", "left", "shift+tab":
		m.switchResourceType()
		return m, nil
	case "/":
		return m.startFilter()
	case "o":
		return m.startSort()
	case "x":
		return m.startExport()
	case "r":
		return m.startRefresh()
	case "s":
		return m.startEndpointSelection()
	case "enter":
		if m.selectedResource() != nil {
			m.screen = screenDetail
			m.refreshDetail()
		}
		return m, nil
	case "e":
		return m.startEdit(screenBrowse)
	case "d":
		return m.startDelete(screenBrowse)
	case " ":
		if m.activeType == "Users" {
			return m.startToggleActive(screenBrowse)
		}
	case "m":
		if m.activeType == "Groups" {
			return m.startMembers(screenBrowse)
		}
	case "?":
		m.helpReturn, m.screen = screenBrowse, screenHelp
		return m, nil
	}
	var command tea.Cmd
	m.table, command = m.table.Update(key)
	return m, command
}

func (m Model) updateDetail(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace":
		m.screen = screenBrowse
		return m, nil
	case "e":
		return m.startEdit(screenDetail)
	case "c":
		return m.copySelectedResource()
	case "d":
		return m.startDelete(screenDetail)
	case " ":
		if m.activeType == "Users" {
			return m.startToggleActive(screenDetail)
		}
	case "m":
		if m.activeType == "Groups" {
			return m.startMembers(screenDetail)
		}
	case "r":
		return m.startRefresh()
	case "s":
		return m.startEndpointSelection()
	case "?":
		m.helpReturn, m.screen = screenDetail, screenHelp
		return m, nil
	}
	var command tea.Cmd
	m.detail, command = m.detail.Update(key)
	return m, command
}

func (m Model) updateFilter(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.filterInput.Blur()
		m.errorText = ""
		m.status = "Filter unchanged"
		m.screen = screenBrowse
		return m, nil
	case "enter":
		candidate := strings.TrimSpace(m.filterInput.Value())
		if _, err := parseLocalFilter(m.activeType, candidate); err != nil {
			m.errorText = err.Error()
			m.status = "Filter not applied"
			return m, nil
		}
		preserveID := ""
		if selected := m.selectedResource(); selected != nil {
			preserveID = stringValue((*selected)["id"])
		}
		m.setCurrentFilter(candidate)
		m.filterInput.Blur()
		m.applyFilter(preserveID)
		m.screen = screenBrowse
		m.errorText = ""
		if candidate == "" {
			m.status = "Filter cleared"
		} else {
			m.status = fmt.Sprintf("Showing %d local matches", len(m.visible))
		}
		return m, nil
	}
	var command tea.Cmd
	m.filterInput, command = m.filterInput.Update(key)
	m.errorText = ""
	return m, command
}

func (m Model) updateSort(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	options := sortOptions(m.activeType)
	switch key.String() {
	case "esc", "backspace":
		m.screen = screenBrowse
		return m, nil
	case "up", "k":
		m.sortIndex = (m.sortIndex - 1 + len(options)) % len(options)
		return m, nil
	case "down", "j":
		m.sortIndex = (m.sortIndex + 1) % len(options)
		return m, nil
	case "left":
		m.sortDescending = false
		return m, nil
	case "right":
		m.sortDescending = true
		return m, nil
	case "space", " ":
		m.sortDescending = !m.sortDescending
		return m, nil
	case "enter":
		preserveID := ""
		if selected := m.selectedResource(); selected != nil {
			preserveID = stringValue((*selected)["id"])
		}
		spec := sortSpec{key: options[m.sortIndex].key, descending: m.sortDescending}
		m.setCurrentSort(spec)
		m.applyFilter(preserveID)
		m.errorText = ""
		direction := "ascending"
		if spec.descending {
			direction = "descending"
		}
		m.status = fmt.Sprintf("Sorted %s by %s %s", strings.ToLower(m.activeType), options[m.sortIndex].label, direction)
		m.screen = screenBrowse
		return m, nil
	}
	return m, nil
}

func (m Model) updateExport(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.exportInput.Blur()
		m.errorText = ""
		m.status = "Export cancelled"
		m.screen = screenBrowse
		return m, nil
	case "enter", "ctrl+s":
		path := strings.TrimSpace(m.exportInput.Value())
		if path == "" {
			m.errorText = "Enter an export file path"
			return m, nil
		}
		resources := append([]scim.Resource(nil), m.visible...)
		m.exportInput.Blur()
		m.busy = true
		m.errorText = ""
		m.status = fmt.Sprintf("Exporting %d %s…", len(resources), strings.ToLower(m.activeType))
		return m, tea.Batch(m.spinner.Tick, exportResourcesCmd(path, m.activeType, resources))
	}
	var command tea.Cmd
	m.exportInput, command = m.exportInput.Update(key)
	m.errorText = ""
	return m, command
}

func (m Model) updateEdit(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.blurEditInputs()
		m.screen = m.returnTo
		return m, nil
	case "tab", "down":
		return m.focusEditField(1)
	case "shift+tab", "up":
		return m.focusEditField(-1)
	case "ctrl+s":
		return m.submitEdit()
	case "enter":
		if m.editIndex == len(m.editInputs)-1 {
			return m.submitEdit()
		}
		return m.focusEditField(1)
	}
	var command tea.Cmd
	m.editInputs[m.editIndex], command = m.editInputs[m.editIndex].Update(key)
	return m, command
}

func (m Model) updateConfirm(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch key.String() {
	case "y", "Y":
		m.busy = true
		m.errorText = ""
		m.status = "Applying change…"
		return m, tea.Batch(m.spinner.Tick, m.confirmCommand())
	case "n", "N", "esc":
		m.screen = m.confirm.returnTo
		m.status = "Action cancelled"
		return m, nil
	}
	return m, nil
}

func (m Model) updateMembers(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace":
		m.screen = m.returnTo
		return m, nil
	case "a":
		m.memberInput.Reset()
		m.memberInput.SetWidth(max(24, min(60, m.width-16)))
		command := m.memberInput.Focus()
		m.screen = screenMemberAdd
		return m, command
	case "d", "x":
		member := m.selectedMember()
		if member == nil {
			return m, nil
		}
		memberID := stringValue((*member)["value"])
		if memberID == "" {
			m.errorText = "Selected member does not have a value ID"
			return m, nil
		}
		m.confirm = pendingAction{
			kind: actionRemoveMember, resourceType: "Groups", id: m.memberGroup,
			memberID: memberID, ifMatch: m.resourceVersion("Groups", m.memberGroup),
			label: "Remove member " + m.memberDisplay(*member) + "?", returnTo: screenMembers,
		}
		m.screen = screenConfirm
		return m, nil
	case "r":
		m.busy = true
		m.status = "Refreshing group membership…"
		return m, tea.Batch(m.spinner.Tick, loadGroupCmd(m.ctx, m.client, m.memberGroup, m.returnTo))
	case "?":
		m.helpReturn, m.screen = screenMembers, screenHelp
		return m, nil
	}
	var command tea.Cmd
	m.memberTable, command = m.memberTable.Update(key)
	return m, command
}

func (m Model) updateMemberAdd(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.memberInput.Blur()
		m.screen = screenMembers
		return m, nil
	case "enter":
		query := strings.TrimSpace(m.memberInput.Value())
		if query == "" {
			m.errorText = "Enter a user ID or exact username"
			return m, nil
		}
		userID := m.resolveUserID(query)
		m.memberInput.Blur()
		m.busy = true
		m.errorText = ""
		m.status = "Adding group member…"
		return m, tea.Batch(m.spinner.Tick, addMemberCmd(m.ctx, m.client, m.memberGroup, userID, m.resourceVersion("Groups", m.memberGroup)))
	}
	var command tea.Cmd
	m.memberInput, command = m.memberInput.Update(key)
	return m, command
}

func (m Model) handleResourcesLoaded(msg resourcesLoadedMsg) (tea.Model, tea.Cmd) {
	if m.pendingLoad > 0 {
		m.pendingLoad--
	}
	if msg.err != nil {
		m.errorText = fmt.Sprintf("Could not load %s: %v", strings.ToLower(msg.resourceType), msg.err)
	} else {
		m.setResources(msg.resourceType, msg.resources)
		if msg.resourceType == m.activeType {
			m.applyFilter(m.refreshID)
			if m.screen == screenDetail {
				m.refreshDetail()
			}
			m.refreshID = ""
		}
	}
	if m.pendingLoad == 0 {
		m.busy = false
		if m.errorText == "" {
			m.status = fmt.Sprintf("Loaded %d users and %d groups", len(m.users), len(m.groups))
		}
		return m, nil
	}
	return m, m.spinner.Tick
}

func (m Model) handleGroupLoaded(msg groupLoadedMsg) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.errorText = msg.err.Error()
		m.screen = msg.returnTo
		m.status = "Could not load group membership"
		return m, nil
	}
	m.upsertResource("Groups", msg.group)
	if m.activeType == "Groups" {
		m.applyFilter(stringValue(msg.group["id"]))
	}
	m.memberGroup = stringValue(msg.group["id"])
	m.members, msg.err = memberResources(msg.group)
	if msg.err != nil {
		m.errorText = msg.err.Error()
		m.screen = msg.returnTo
		return m, nil
	}
	memberRows := m.memberRows()
	m.memberTable.SetRows(memberRows)
	m.memberTable.SetCursor(0)
	m.screen = screenMembers
	m.status = fmt.Sprintf("%d group members", len(m.members))
	return m, nil
}

func (m Model) handleMutation(msg mutationFinishedMsg) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.errorText = msg.err.Error()
		m.status = "Change failed"
		m.screen = msg.failure
		if msg.failure == screenEdit && len(m.editInputs) > 0 {
			return m, m.editInputs[m.editIndex].Focus()
		}
		if msg.failure == screenMemberAdd {
			return m, m.memberInput.Focus()
		}
		return m, nil
	}
	m.errorText = ""
	if msg.deleted {
		m.removeResource(msg.resourceType, msg.id)
		m.applyFilter("")
		m.screen = screenBrowse
	} else {
		m.upsertResource(msg.resourceType, msg.resource)
		if msg.resourceType == m.activeType {
			m.applyFilter(msg.id)
		}
		if msg.next == screenMembers {
			members, err := memberResources(msg.resource)
			if err != nil {
				m.errorText = err.Error()
				m.screen = screenBrowse
				return m, nil
			}
			m.members = members
			memberRows := m.memberRows()
			m.memberTable.SetRows(memberRows)
			m.memberTable.SetCursor(0)
		}
		next := msg.next
		if next == screenDetail {
			selected := m.selectedResource()
			if selected == nil || stringValue((*selected)["id"]) != msg.id {
				next = screenBrowse
			} else {
				m.refreshDetail()
			}
		}
		m.screen = next
	}
	m.status = msg.status
	return m, nil
}

func (m Model) handleExportFinished(msg exportFinishedMsg) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.errorText = msg.err.Error()
		m.status = "Export failed"
		m.screen = screenExport
		return m, m.exportInput.Focus()
	}
	m.errorText = ""
	m.screen = screenBrowse
	m.status = fmt.Sprintf("Exported %d %s to %s", msg.count, strings.ToLower(msg.resourceType), msg.path)
	return m, nil
}

func (m Model) startFilter() (tea.Model, tea.Cmd) {
	m.filterInput.SetValue(m.currentFilter())
	m.filterInput.CursorEnd()
	m.filterInput.SetWidth(max(24, min(60, m.width-16)))
	command := m.filterInput.Focus()
	m.errorText = ""
	m.screen = screenFilter
	return m, command
}

func (m Model) startSort() (tea.Model, tea.Cmd) {
	spec := m.currentSort()
	m.sortIndex = 0
	for index, option := range sortOptions(m.activeType) {
		if option.key == spec.key {
			m.sortIndex = index
			break
		}
	}
	m.sortDescending = spec.descending
	m.errorText = ""
	m.screen = screenSort
	return m, nil
}

func (m Model) startExport() (tea.Model, tea.Cmd) {
	m.exportInput.SetValue(strings.ToLower(m.activeType) + ".json")
	m.exportInput.CursorEnd()
	m.exportInput.SetWidth(max(24, min(60, m.width-16)))
	command := m.exportInput.Focus()
	m.errorText = ""
	m.screen = screenExport
	return m, command
}

func (m Model) startRefresh() (tea.Model, tea.Cmd) {
	if selected := m.selectedResource(); selected != nil {
		m.refreshID = stringValue((*selected)["id"])
	}
	m.pendingLoad += 2
	m.busy = true
	m.errorText = ""
	m.status = "Refreshing users and groups…"
	return m, tea.Batch(
		m.spinner.Tick,
		loadResourcesCmd(m.ctx, m.client, "Users"),
		loadResourcesCmd(m.ctx, m.client, "Groups"),
	)
}

func (m Model) startEdit(returnTo screen) (tea.Model, tea.Cmd) {
	resource := m.selectedResource()
	if resource == nil {
		return m, nil
	}
	m.returnTo = returnTo
	m.editFields = editableFields(m.activeType, *resource)
	m.editInputs = make([]textinput.Model, len(m.editFields))
	for index, field := range m.editFields {
		input := newInput(field.label)
		input.SetValue(field.original)
		input.SetWidth(max(24, min(64, m.width-20)))
		m.editInputs[index] = input
	}
	m.editIndex = 0
	command := m.editInputs[0].Focus()
	m.screen = screenEdit
	return m, command
}

func (m Model) submitEdit() (tea.Model, tea.Cmd) {
	resource := m.selectedResource()
	if resource == nil {
		m.screen = m.returnTo
		return m, nil
	}
	operations, err := editOperations(m.editFields, m.editInputs)
	if err != nil {
		m.errorText = err.Error()
		return m, nil
	}
	if len(operations) == 0 {
		m.status = "No changes to save"
		m.blurEditInputs()
		m.screen = m.returnTo
		return m, nil
	}
	id := stringValue((*resource)["id"])
	if id == "" {
		m.errorText = "Selected resource has no ID"
		return m, nil
	}
	m.blurEditInputs()
	m.busy = true
	m.errorText = ""
	m.status = "Saving and verifying changes…"
	next := m.returnTo
	return m, tea.Batch(m.spinner.Tick, patchAndGetCmd(
		m.ctx, m.client, m.activeType, id, resourceVersion(*resource), operations,
		"Saved changes", next, screenEdit,
	))
}

func (m Model) focusEditField(delta int) (tea.Model, tea.Cmd) {
	m.editInputs[m.editIndex].Blur()
	m.editIndex = (m.editIndex + delta + len(m.editInputs)) % len(m.editInputs)
	return m, m.editInputs[m.editIndex].Focus()
}

func (m *Model) blurEditInputs() {
	for index := range m.editInputs {
		m.editInputs[index].Blur()
	}
}

func (m Model) startDelete(returnTo screen) (tea.Model, tea.Cmd) {
	resource := m.selectedResource()
	if resource == nil {
		return m, nil
	}
	id := stringValue((*resource)["id"])
	m.confirm = pendingAction{
		kind: actionDelete, resourceType: m.activeType, id: id,
		ifMatch: resourceVersion(*resource),
		label:   "Delete " + resourceLabel(m.activeType, *resource) + "? This cannot be undone.", returnTo: returnTo,
	}
	m.screen = screenConfirm
	return m, nil
}

func (m Model) startToggleActive(returnTo screen) (tea.Model, tea.Cmd) {
	resource := m.selectedResource()
	if resource == nil {
		return m, nil
	}
	active, _ := (*resource)["active"].(bool)
	verb := "Activate "
	if active {
		verb = "Deactivate "
	}
	m.confirm = pendingAction{
		kind: actionToggleActive, resourceType: "Users", id: stringValue((*resource)["id"]),
		active: !active, ifMatch: resourceVersion(*resource),
		label: verb + resourceLabel("Users", *resource) + "?", returnTo: returnTo,
	}
	m.screen = screenConfirm
	return m, nil
}

func (m Model) startMembers(returnTo screen) (tea.Model, tea.Cmd) {
	resource := m.selectedResource()
	if resource == nil {
		return m, nil
	}
	id := stringValue((*resource)["id"])
	m.memberGroup = id
	m.returnTo = returnTo
	m.busy = true
	m.errorText = ""
	m.status = "Loading group membership…"
	return m, tea.Batch(m.spinner.Tick, loadGroupCmd(m.ctx, m.client, id, returnTo))
}

func (m Model) confirmCommand() tea.Cmd {
	action := m.confirm
	switch action.kind {
	case actionDelete:
		return deleteCmd(m.ctx, m.client, action.resourceType, action.id, action.ifMatch)
	case actionToggleActive:
		operations := []any{map[string]any{"op": "replace", "path": "active", "value": action.active}}
		status := "User deactivated"
		if action.active {
			status = "User activated"
		}
		return patchAndGetCmd(m.ctx, m.client, "Users", action.id, action.ifMatch, operations, status, action.returnTo, action.returnTo)
	case actionRemoveMember:
		path := fmt.Sprintf(`members[value eq "%s"]`, escapeFilterString(action.memberID))
		operations := []any{map[string]any{"op": "remove", "path": path}}
		return patchAndGetCmd(m.ctx, m.client, "Groups", action.id, action.ifMatch, operations, "Group member removed", screenMembers, screenMembers)
	default:
		return func() tea.Msg {
			return mutationFinishedMsg{err: errors.New("unknown action"), failure: action.returnTo}
		}
	}
}

func (m *Model) switchResourceType() {
	if m.activeType == "Users" {
		m.activeType = "Groups"
	} else {
		m.activeType = "Users"
	}
	m.applyFilter("")
	m.errorText = ""
	m.status = fmt.Sprintf("%d %s", len(m.visible), strings.ToLower(m.activeType))
}

func (m *Model) resize(width, height int) {
	m.width = max(width, 40)
	m.height = max(height, 14)
	contentHeight := max(7, m.height-9)
	if m.width >= 96 {
		leftWidth := max(45, (m.width-5)*3/5)
		m.table.SetWidth(leftWidth - 4)
	} else {
		m.table.SetWidth(m.width - 6)
	}
	m.table.SetHeight(contentHeight - 3)
	m.memberTable.SetWidth(m.width - 8)
	m.memberTable.SetHeight(contentHeight - 2)
	m.detail.SetWidth(max(30, m.width-8))
	m.detail.SetHeight(contentHeight)
	for index := range m.editInputs {
		m.editInputs[index].SetWidth(max(24, min(64, m.width-20)))
	}
	m.exportInput.SetWidth(max(24, min(60, m.width-16)))
	_, endpointInputWidth := m.endpointFormWidths()
	for index := range m.endpointInputs {
		m.endpointInputs[index].SetWidth(endpointInputWidth)
	}
	m.refreshColumns()
}

func newInput(placeholder string) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.CharLimit = 1024
	input.SetVirtualCursor(true)
	input.SetWidth(48)
	return input
}

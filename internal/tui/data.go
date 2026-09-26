package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"pool-skimmer/internal/scim"
)

func (m *Model) setResources(resourceType string, resources []scim.Resource) {
	resources = append([]scim.Resource(nil), resources...)
	if resourceType == "Users" {
		m.users = resources
	} else {
		m.groups = resources
	}
}

func (m Model) resourcesFor(resourceType string) []scim.Resource {
	if resourceType == "Users" {
		return m.users
	}
	return m.groups
}

func (m *Model) applyFilter(preserveID string) {
	filter, err := parseLocalFilter(m.activeType, m.currentFilter())
	if err != nil {
		m.errorText = err.Error()
		return
	}
	resources := m.resourcesFor(m.activeType)
	visible := make([]scim.Resource, 0, len(resources))
	for _, resource := range resources {
		if filter.matches(m.activeType, resource) {
			visible = append(visible, resource)
		}
	}
	sortVisibleResources(visible, m.activeType, m.currentSort())
	m.visible = visible
	m.refreshColumns()
	m.table.SetRows(m.resourceRows())
	m.table.SetCursor(0)
	if preserveID != "" {
		for index, resource := range m.visible {
			if stringValue(resource["id"]) == preserveID {
				m.table.SetCursor(index)
				break
			}
		}
	}
}

func (m Model) resourceRows() []table.Row {
	rows := make([]table.Row, 0, len(m.visible))
	for _, resource := range m.visible {
		if m.activeType == "Users" {
			active := "Disabled"
			if value, _ := resource["active"].(bool); value {
				active = "Enabled"
			}
			rows = append(rows, table.Row{
				clean(resourceLabel("Users", resource)),
				clean(stringValue(resource["userName"])),
				active,
			})
		} else {
			rows = append(rows, table.Row{
				clean(resourceLabel("Groups", resource)),
				strconv.Itoa(arrayLength(resource["members"])),
				clean(stringValue(resource["id"])),
			})
		}
	}
	return rows
}

func (m *Model) refreshColumns() {
	width := max(36, m.table.Width()-2)
	if m.activeType == "Users" {
		nameWidth := max(12, width*35/100)
		statusWidth := 10
		usernameWidth := max(14, width-nameWidth-statusWidth-6)
		m.table.SetColumns([]table.Column{
			{Title: m.sortColumnTitle("NAME", "name"), Width: nameWidth},
			{Title: m.sortColumnTitle("USERNAME", "username"), Width: usernameWidth},
			{Title: m.sortColumnTitle("STATUS", "status"), Width: statusWidth},
		})
	} else {
		nameWidth := max(15, width*45/100)
		memberWidth := 9
		idWidth := max(14, width-nameWidth-memberWidth-6)
		m.table.SetColumns([]table.Column{
			{Title: m.sortColumnTitle("GROUP", "name"), Width: nameWidth},
			{Title: m.sortColumnTitle("MEMBERS", "members"), Width: memberWidth},
			{Title: m.sortColumnTitle("ID", "id"), Width: idWidth},
		})
	}
}

func (m Model) selectedResource() *scim.Resource {
	index := m.table.Cursor()
	if index < 0 || index >= len(m.visible) {
		return nil
	}
	resource := m.visible[index]
	return &resource
}

func (m *Model) upsertResource(resourceType string, updated scim.Resource) {
	id := stringValue(updated["id"])
	resources := m.resourcesFor(resourceType)
	found := false
	for index := range resources {
		if stringValue(resources[index]["id"]) == id {
			resources[index] = updated
			found = true
			break
		}
	}
	if !found {
		resources = append(resources, updated)
	}
	m.setResources(resourceType, resources)
}

func (m *Model) removeResource(resourceType, id string) {
	resources := m.resourcesFor(resourceType)
	filtered := resources[:0]
	for _, resource := range resources {
		if stringValue(resource["id"]) != id {
			filtered = append(filtered, resource)
		}
	}
	m.setResources(resourceType, filtered)
}

func editableFields(resourceType string, resource scim.Resource) []editField {
	if resourceType == "Groups" {
		return []editField{{label: "Display name", path: "displayName", original: rawString(resource["displayName"])}}
	}
	active := "false"
	if value, _ := resource["active"].(bool); value {
		active = "true"
	}
	return []editField{
		{label: "Username", path: "userName", original: rawString(resource["userName"])},
		{label: "Display name", path: "displayName", original: rawString(resource["displayName"])},
		{label: "Given name", path: "name.givenName", original: nestedString(resource, "name", "givenName")},
		{label: "Family name", path: "name.familyName", original: nestedString(resource, "name", "familyName")},
		{label: "Active (true/false)", path: "active", original: active},
	}
}

func editOperations(fields []editField, inputs []textinput.Model) ([]any, error) {
	if len(fields) != len(inputs) {
		return nil, errors.New("edit form is inconsistent")
	}
	operations := make([]any, 0, len(fields))
	for index, field := range fields {
		value := strings.TrimSpace(inputs[index].Value())
		if value == strings.TrimSpace(field.original) {
			continue
		}
		var typedValue any = value
		if field.path == "active" {
			parsed, err := strconv.ParseBool(strings.ToLower(value))
			if err != nil {
				return nil, errors.New("active must be true or false")
			}
			typedValue = parsed
		}
		if (field.path == "userName" || (len(fields) == 1 && field.path == "displayName")) && value == "" {
			return nil, fmt.Errorf("%s must not be empty", strings.ToLower(field.label))
		}
		operations = append(operations, map[string]any{
			"op": "replace", "path": field.path, "value": typedValue,
		})
	}
	return operations, nil
}

func memberResources(group scim.Resource) ([]scim.Resource, error) {
	raw := group["members"]
	if raw == nil {
		return []scim.Resource{}, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("group members was not an array")
	}
	members := make([]scim.Resource, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("group members contained a non-object value")
		}
		members = append(members, scim.Resource(object))
	}
	return members, nil
}

func (m *Model) memberRows() []table.Row {
	rows := make([]table.Row, 0, len(m.members))
	for _, member := range m.members {
		rows = append(rows, table.Row{
			clean(m.memberDisplay(member)),
			clean(stringValue(member["value"])),
			clean(stringValue(member["type"])),
		})
	}
	width := max(36, m.memberTable.Width()-2)
	nameWidth := max(16, width*35/100)
	typeWidth := 10
	idWidth := max(14, width-nameWidth-typeWidth-6)
	m.memberTable.SetColumns([]table.Column{
		{Title: "MEMBER", Width: nameWidth},
		{Title: "USER ID", Width: idWidth},
		{Title: "TYPE", Width: typeWidth},
	})
	return rows
}

func (m Model) selectedMember() *scim.Resource {
	index := m.memberTable.Cursor()
	if index < 0 || index >= len(m.members) {
		return nil
	}
	member := m.members[index]
	return &member
}

func (m Model) memberDisplay(member scim.Resource) string {
	if display := stringValue(member["display"]); display != "" {
		return display
	}
	id := stringValue(member["value"])
	for _, user := range m.users {
		if stringValue(user["id"]) == id {
			return resourceLabel("Users", user)
		}
	}
	return id
}

func (m Model) resolveUserID(query string) string {
	for _, user := range m.users {
		if stringValue(user["id"]) == query || strings.EqualFold(stringValue(user["userName"]), query) {
			return stringValue(user["id"])
		}
	}
	return query
}

func (m Model) resourceVersion(resourceType, id string) string {
	for _, resource := range m.resourcesFor(resourceType) {
		if stringValue(resource["id"]) == id {
			return resourceVersion(resource)
		}
	}
	return ""
}

func resourceVersion(resource scim.Resource) string {
	meta, ok := resource["meta"].(map[string]any)
	if !ok {
		return ""
	}
	return stringValue(meta["version"])
}

func (m *Model) refreshDetail() {
	resource := m.selectedResource()
	if resource == nil {
		m.detail.SetContent("No resource selected")
		return
	}
	content, err := resourceJSON(*resource)
	if err != nil {
		m.detail.SetContent("Could not render resource: " + err.Error())
		return
	}
	m.detail.SetContent(content)
	m.detail.GotoTop()
}

func (m Model) copySelectedResource() (tea.Model, tea.Cmd) {
	resource := m.selectedResource()
	if resource == nil {
		m.errorText = "No resource selected"
		return m, nil
	}
	content, err := resourceJSON(*resource)
	if err != nil {
		m.errorText = "Could not copy resource: " + err.Error()
		return m, nil
	}
	if m.setClipboard == nil {
		m.errorText = "Clipboard support is unavailable"
		return m, nil
	}
	m.errorText = ""
	m.status = "Copied resource JSON to clipboard"
	return m, m.setClipboard(content)
}

func resourceJSON(resource scim.Resource) (string, error) {
	data, err := json.MarshalIndent(resource, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func resourceLabel(resourceType string, resource scim.Resource) string {
	if resourceType == "Groups" {
		if value := stringValue(resource["displayName"]); value != "" {
			return value
		}
	} else {
		if value := stringValue(resource["displayName"]); value != "" {
			return value
		}
		if name := structuredName(resource); name != "" {
			return name
		}
		if value := stringValue(resource["userName"]); value != "" {
			return value
		}
	}
	return stringValue(resource["id"])
}

func structuredName(resource scim.Resource) string {
	name, ok := resource["name"].(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(rawString(name["givenName"]) + " " + rawString(name["familyName"]))
}

func nestedString(resource scim.Resource, object, key string) string {
	nested, ok := resource[object].(map[string]any)
	if !ok {
		return ""
	}
	return rawString(nested[key])
}

func searchText(resource scim.Resource) string {
	parts := []string{
		resourceLabel("Users", resource),
		resourceLabel("Groups", resource),
		stringValue(resource["id"]),
		stringValue(resource["userName"]),
		stringValue(resource["externalId"]),
		structuredName(resource),
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func stringValue(value any) string {
	return strings.TrimSpace(rawString(value))
}

func rawString(value any) string {
	text, _ := value.(string)
	return text
}

func arrayLength(value any) int {
	items, ok := value.([]any)
	if !ok {
		return 0
	}
	return len(items)
}

func clean(value string) string {
	return strings.Map(func(character rune) rune {
		if character == '\t' || character == '\n' || character == '\r' || unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
}

func truncate(value string, width int) string {
	value = clean(value)
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	runes := []rune(value)
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func singular(resourceType string) string {
	return strings.ToLower(strings.TrimSuffix(resourceType, "s"))
}

func escapeFilterString(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`)
}

package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"pool-skimmer/internal/scim"
)

var (
	accent       = lipgloss.Color("#8B7CF6")
	cyan         = lipgloss.Color("#39D0C3")
	green        = lipgloss.Color("#57D39B")
	amber        = lipgloss.Color("#F4BF75")
	red          = lipgloss.Color("#FF6B81")
	muted        = lipgloss.Color("#7F849C")
	text         = lipgloss.Color("#CDD6F4")
	surface      = lipgloss.Color("#313244")
	accentStyle  = lipgloss.NewStyle().Foreground(accent).Bold(true)
	titleStyle   = lipgloss.NewStyle().Foreground(accent).Bold(true)
	mutedStyle   = lipgloss.NewStyle().Foreground(muted)
	labelStyle   = lipgloss.NewStyle().Foreground(cyan).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(red).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(green)
)

func tableStyles() table.Styles {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		Foreground(cyan).
		Bold(true).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(surface)
	styles.Cell = styles.Cell.Foreground(text).Padding(0, 1)
	styles.Selected = styles.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(accent).
		Bold(true)
	return styles
}

// View renders the current application state.
func (m Model) View() tea.View {
	body := ""
	switch m.screen {
	case screenEndpointSelect:
		body = m.renderEndpointSelect()
	case screenEndpointCreate:
		body = m.renderEndpointForm()
	case screenBrowse:
		body = m.renderBrowser()
	case screenDetail:
		body = m.renderDetail()
	case screenMembers:
		body = m.renderMembers()
	case screenFilter:
		body = m.renderFilter()
	case screenSort:
		body = m.renderSort()
	case screenExport:
		body = m.renderExport()
	case screenEdit:
		body = m.renderEdit()
	case screenConfirm:
		body = m.renderConfirm()
	case screenMemberAdd:
		body = m.renderModal("Add group member", m.memberInput.View()+"\n\n"+mutedStyle.Render("An exact loaded username is resolved to its SCIM user ID."))
	case screenHelp:
		body = m.renderHelp()
	}

	content := strings.Join([]string{
		m.renderHeader(),
		body,
		m.renderStatus(),
		m.renderFooter(),
	}, "\n")
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "Pool Skimmer · SCIM"
	return view
}

func (m Model) renderHeader() string {
	title := titleStyle.Render("◈  POOL SKIMMER") + " " + mutedStyle.Render("v"+m.version)
	configuredEndpoint := m.endpoint
	if m.endpointName != "" {
		configuredEndpoint = m.endpointName + " · " + configuredEndpoint
	}
	endpoint := mutedStyle.Render(truncate(configuredEndpoint, max(12, m.width-lipgloss.Width(title)-4)))
	gap := strings.Repeat(" ", max(1, m.width-lipgloss.Width(title)-lipgloss.Width(endpoint)))
	line := title + gap + endpoint
	if m.screen == screenEndpointSelect || m.screen == screenEndpointCreate {
		return line
	}

	users := m.tab("Users", len(m.users), m.activeType == "Users")
	groups := m.tab("Groups", len(m.groups), m.activeType == "Groups")
	filter := ""
	if m.currentFilter() != "" {
		filter = "  " + lipgloss.NewStyle().Foreground(amber).Render("filter: "+truncate(m.currentFilter(), 32))
	}
	return line + "\n" + users + " " + groups + filter
}

func (m Model) renderEndpointSelect() string {
	rows := make([]string, 0, len(m.endpoints)+3)
	if len(m.endpoints) == 0 {
		rows = append(rows, mutedStyle.Render("No SCIM endpoints are configured yet."), "")
	}
	for index, endpoint := range m.endpoints {
		marker := "  "
		nameStyle := lipgloss.NewStyle().Foreground(text)
		if index == m.endpointIndex {
			marker = "› "
			nameStyle = accentStyle
		}
		rows = append(rows, nameStyle.Render(marker+clean(endpoint.Name))+"  "+mutedStyle.Render(truncate(clean(endpoint.URL), 50)))
	}
	createMarker := "  "
	createStyle := lipgloss.NewStyle().Foreground(text)
	if m.endpointIndex == len(m.endpoints) {
		createMarker = "› "
		createStyle = accentStyle
	}
	rows = append(rows, createStyle.Render(createMarker+"+ Create an Endpoint"))
	return m.renderModal("Select a SCIM Endpoint or Create an Endpoint", strings.Join(rows, "\n"))
}

func (m Model) renderEndpointForm() string {
	title := "Create a SCIM Endpoint"
	if m.editingEndpointID != "" {
		title = "Edit SCIM Endpoint"
	}
	labelWidth, _ := m.endpointFormWidths()
	rows := make([]string, 0, len(endpointInputLabels)+2)
	for index, label := range endpointInputLabels {
		style := mutedStyle
		marker := "  "
		if index == m.endpointInputIndex {
			style = labelStyle
			marker = "› "
		}
		styledLabel := style.Width(labelWidth).Render(marker + truncate(label, labelWidth-2))
		rows = append(rows, styledLabel+" "+m.endpointInputs[index].View())
	}
	rows = append(rows, "", mutedStyle.Render("API keys use separate mode-0600 files, not config.json."))
	return m.renderModal(title, strings.Join(rows, "\n"))
}

func (m Model) tab(name string, count int, active bool) string {
	content := fmt.Sprintf(" %s  %d ", name, count)
	if active {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Bold(true).Render(content)
	}
	return lipgloss.NewStyle().Foreground(muted).Background(surface).Render(content)
}

func (m Model) renderBrowser() string {
	height := max(7, m.height-8)
	leftWidth := m.width - 2
	if m.width >= 96 {
		leftWidth = max(45, (m.width-5)*3/5)
	}
	listTitle := fmt.Sprintf("%s  %s", labelStyle.Render(m.activeType), mutedStyle.Render(fmt.Sprintf("%d shown", len(m.visible))))
	listContent := listTitle + "\n" + m.table.View()
	left := panel(false).Width(leftWidth - 2).Height(height - 2).Render(listContent)
	if m.width < 96 {
		return left
	}

	rightWidth := max(30, m.width-leftWidth-3)
	right := panel(true).Width(rightWidth - 2).Height(height - 2).Render(m.renderSelectedSummary(rightWidth - 6))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func (m Model) renderSelectedSummary(width int) string {
	resource := m.selectedResource()
	if resource == nil {
		return lipgloss.Place(width, 8, lipgloss.Center, lipgloss.Center, mutedStyle.Render("No resource selected"))
	}
	r := *resource
	lines := []string{
		labelStyle.Render(truncate(resourceLabel(m.activeType, r), width)),
		"",
		definition("ID", stringValue(r["id"]), width),
	}
	if m.activeType == "Users" {
		lines = append(lines,
			definition("Username", stringValue(r["userName"]), width),
			definition("Given name", nestedString(r, "name", "givenName"), width),
			definition("Family name", nestedString(r, "name", "familyName"), width),
			definition("Status", activeLabel(r), width),
		)
	} else {
		lines = append(lines,
			definition("Display name", stringValue(r["displayName"]), width),
			definition("Members", fmt.Sprint(arrayLength(r["members"])), width),
		)
	}
	if meta, ok := r["meta"].(map[string]any); ok {
		lines = append(lines,
			"",
			definition("Created", stringValue(meta["created"]), width),
			definition("Modified", stringValue(meta["lastModified"]), width),
		)
	}
	lines = append(lines, "", mutedStyle.Render("Enter opens the complete SCIM resource."))
	return strings.Join(lines, "\n")
}

func (m Model) renderDetail() string {
	height := max(7, m.height-8)
	resource := m.selectedResource()
	title := "Resource details"
	if resource != nil {
		title = resourceLabel(m.activeType, *resource)
	}
	content := labelStyle.Render(truncate(title, m.width-8)) + "\n" + m.detail.View()
	return panel(true).Width(m.width - 4).Height(height - 2).Render(content)
}

func (m Model) renderMembers() string {
	height := max(7, m.height-8)
	groupName := m.memberGroup
	for _, group := range m.groups {
		if stringValue(group["id"]) == m.memberGroup {
			groupName = resourceLabel("Groups", group)
			break
		}
	}
	title := labelStyle.Render(truncate(groupName, m.width-20)) + "  " + mutedStyle.Render(fmt.Sprintf("%d members", len(m.members)))
	content := title + "\n" + m.memberTable.View()
	return panel(true).Width(m.width - 4).Height(height - 2).Render(content)
}

func (m Model) renderEdit() string {
	resource := m.selectedResource()
	title := "Edit resource"
	if resource != nil {
		title = "Edit " + resourceLabel(m.activeType, *resource)
	}
	rows := make([]string, 0, len(m.editFields)*2)
	for index, field := range m.editFields {
		style := mutedStyle
		marker := "  "
		if index == m.editIndex {
			style = labelStyle
			marker = "› "
		}
		rows = append(rows, style.Render(marker+field.label), "  "+m.editInputs[index].View())
	}
	rows = append(rows, "", mutedStyle.Render("Tab moves between fields · Enter advances · Ctrl+S saves"))
	return m.renderModal(title, strings.Join(rows, "\n"))
}

func (m Model) renderConfirm() string {
	content := lipgloss.NewStyle().Foreground(text).Bold(true).Render(m.confirm.label)
	if m.busy {
		content += "\n\n" + m.spinner.View() + " Applying and verifying…"
	} else {
		content += "\n\n" + key("y") + " confirm    " + key("n / esc") + " cancel"
	}
	return m.renderModal("Confirm action", content)
}

func (m Model) renderFilter() string {
	example := "Free text or: enabled=true  username~@example.com"
	if m.activeType == "Groups" {
		example = "Free text or: name~platform  members>0"
	}
	return m.renderModal("Filter "+strings.ToLower(m.activeType), m.filterInput.View()+"\n\n"+mutedStyle.Render(example))
}

func (m Model) renderSort() string {
	rows := make([]string, 0, len(sortOptions(m.activeType))+2)
	for index, option := range sortOptions(m.activeType) {
		marker := "  "
		style := mutedStyle
		if index == m.sortIndex {
			marker = "› "
			style = labelStyle
		}
		rows = append(rows, style.Render(marker+option.label))
	}
	direction := "Ascending ↑"
	if m.sortDescending {
		direction = "Descending ↓"
	}
	rows = append(rows, "", mutedStyle.Render("Direction: ")+accentStyle.Render(direction))
	return m.renderModal("Sort "+strings.ToLower(m.activeType), strings.Join(rows, "\n"))
}

func (m Model) renderExport() string {
	content := m.exportInput.View() + "\n\n" +
		mutedStyle.Render(fmt.Sprintf("Exports the %d currently shown %s as a JSON array.", len(m.visible), strings.ToLower(m.activeType))) + "\n" +
		mutedStyle.Render("Files use mode 0600 and existing files are not overwritten.")
	return m.renderModal("Export "+strings.ToLower(m.activeType), content)
}

func (m Model) renderHelp() string {
	lines := []string{
		key("↑/↓ or j/k") + " move selection",
		key("tab / ←/→") + " switch users and groups",
		key("enter") + " inspect complete resource",
		key("c") + " copy complete resource JSON",
		key("/") + " filter loaded resources  " + key("o") + " sort by a visible column",
		key("x") + " export currently shown resources as JSON",
		key("e") + " edit selected resource",
		key("space") + " activate or deactivate a user",
		key("m") + " manage group members",
		key("d") + " delete selected resource",
		key("r") + " refresh users and groups",
	}
	if m.endpointStore != nil {
		lines = append(lines, key("s")+" switch SCIM endpoint")
	}
	lines = append(lines, key("q")+" quit")
	return m.renderModal("Keyboard shortcuts", strings.Join(lines, "\n"))
}

func (m Model) renderModal(title, content string) string {
	bodyHeight := max(7, m.height-8)
	modalWidth := max(32, min(74, m.width-8))
	box := lipgloss.NewStyle().
		Width(modalWidth-4).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Render(titleStyle.Render(title) + "\n\n" + content)
	return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderStatus() string {
	content := clean(m.status)
	style := mutedStyle
	if m.errorText != "" {
		content = clean(m.errorText)
		style = errorStyle
	} else if m.pendingLoad > 0 || m.busy {
		content = m.spinner.View() + " " + content
		style = lipgloss.NewStyle().Foreground(amber)
	} else if strings.Contains(strings.ToLower(content), "saved") || strings.Contains(strings.ToLower(content), "deleted") || strings.Contains(strings.ToLower(content), "added") || strings.Contains(strings.ToLower(content), "activated") || strings.Contains(strings.ToLower(content), "copied") || strings.Contains(strings.ToLower(content), "sorted") || strings.Contains(strings.ToLower(content), "exported") {
		style = successStyle
	}
	return style.Width(m.width).Render(" " + ansi.Truncate(content, m.width-2, "…"))
}

func (m Model) renderFooter() string {
	var help string
	switch m.screen {
	case screenEndpointSelect:
		help = key("↑/↓") + " navigate  " + key("enter") + " select  " + key("e") + " edit  " + key("q") + " quit"
		if m.endpointReturn != screenEndpointSelect {
			help += "  " + key("esc") + " back"
		}
	case screenEndpointCreate:
		help = key("ctrl+s") + " save  " + key("tab") + " next field  " + key("esc") + " cancel"
	case screenBrowse:
		if m.busy && m.pendingLoad > 0 {
			help = key("↑/↓") + " navigate  " + key("tab") + " switch  " + key("enter") + " inspect  " + key("?") + " help  " + key("q") + " quit"
			break
		}
		help = key("↑/↓") + " navigate  " + key("tab") + " switch  " + key("/") + " filter  " + key("o") + " sort  " + key("x") + " export  " + key("enter") + " inspect  " + key("e") + " edit  "
		if m.activeType == "Users" {
			help += key("space") + " activate  "
		} else {
			help += key("m") + " members  "
		}
		help += key("d") + " delete  " + key("r") + " refresh  "
		if m.endpointStore != nil {
			help += key("s") + " endpoint  "
		}
		help += key("?") + " help  " + key("q") + " quit"
	case screenDetail:
		if m.busy && m.pendingLoad > 0 {
			help = key("esc") + " back  " + key("↑/↓") + " scroll  " + key("?") + " help  " + key("q") + " quit"
			break
		}
		help = key("esc") + " back  " + key("↑/↓") + " scroll  " + key("c") + " copy JSON  " + key("e") + " edit  " + key("d") + " delete  " + key("r") + " refresh"
		if m.endpointStore != nil {
			help += "  " + key("s") + " endpoint"
		}
	case screenMembers:
		help = key("esc") + " back  " + key("↑/↓") + " navigate  " + key("a") + " add  " + key("d") + " remove  " + key("r") + " refresh"
	case screenFilter:
		help = key("enter") + " apply  " + key("esc") + " cancel"
	case screenSort:
		help = key("↑/↓") + " column  " + key("←/→ or space") + " direction  " + key("enter") + " apply  " + key("esc") + " cancel"
	case screenExport:
		help = key("enter / ctrl+s") + " export  " + key("esc") + " cancel"
	case screenEdit:
		help = key("ctrl+s") + " save  " + key("tab") + " next field  " + key("esc") + " cancel"
	case screenMemberAdd:
		help = key("enter") + " add member  " + key("esc") + " cancel"
	case screenConfirm:
		help = key("y") + " confirm  " + key("n") + " cancel"
	case screenHelp:
		help = key("?") + " close"
	}
	return lipgloss.NewStyle().Foreground(muted).Width(m.width).Render(" " + ansi.Truncate(help, m.width-2, "…"))
}

func panel(active bool) lipgloss.Style {
	color := surface
	if active {
		color = accent
	}
	return lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(color)
}

func definition(label, value string, width int) string {
	if value == "" {
		value = "—"
	}
	return mutedStyle.Render(label+":") + " " + truncate(value, max(4, width-len(label)-2))
}

func activeLabel(resource scim.Resource) string {
	if active, _ := resource["active"].(bool); active {
		return "Enabled"
	}
	return "Disabled"
}

func key(value string) string {
	return lipgloss.NewStyle().Foreground(cyan).Bold(true).Render(value)
}

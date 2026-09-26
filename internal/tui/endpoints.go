package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	endpointconfig "pool-skimmer/internal/config"
	"pool-skimmer/internal/scim"
)

const (
	endpointNameInput = iota
	endpointURLInput
	endpointAPIKeyInput
	endpointAuthHeaderInput
	endpointAuthSchemeInput
	endpointTimeoutInput
	endpointRetriesInput
)

var endpointInputLabels = []string{
	"Name",
	"SCIM endpoint",
	"API key",
	"Authentication header",
	"Authentication scheme",
	"Request timeout",
	"GET read retries",
}

type endpointConnectedMsg struct {
	endpoint endpointconfig.Endpoint
	client   *scim.Client
	failure  screen
	err      error
}

func (m Model) updateEndpointSelect(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	optionCount := len(m.endpoints) + 1
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace":
		if m.endpointReturn != screenEndpointSelect {
			m.screen = m.endpointReturn
			m.errorText = ""
		}
		return m, nil
	case "up", "k":
		m.endpointIndex = (m.endpointIndex - 1 + optionCount) % optionCount
		return m, nil
	case "down", "j", "tab":
		m.endpointIndex = (m.endpointIndex + 1) % optionCount
		return m, nil
	case "e":
		if m.endpointIndex < len(m.endpoints) {
			return m.startEndpointForm(m.endpoints[m.endpointIndex])
		}
		return m, nil
	case "enter":
		if m.endpointIndex == len(m.endpoints) {
			return m.startEndpointForm(endpointconfig.Endpoint{})
		}
		endpoint := m.endpoints[m.endpointIndex]
		m.busy = true
		m.errorText = ""
		m.status = "Connecting to " + endpoint.Name + "…"
		return m, tea.Batch(m.spinner.Tick, connectEndpointCmd(m.endpointStore, endpoint, screenEndpointSelect))
	}
	return m, nil
}

func (m Model) updateEndpointCreate(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.blurEndpointInputs()
		m.screen = screenEndpointSelect
		m.errorText = ""
		return m, nil
	case "tab", "down":
		return m.focusEndpointInput(1)
	case "shift+tab", "up":
		return m.focusEndpointInput(-1)
	case "ctrl+s":
		return m.submitEndpoint()
	case "enter":
		if m.endpointInputIndex == len(m.endpointInputs)-1 {
			return m.submitEndpoint()
		}
		return m.focusEndpointInput(1)
	}
	var command tea.Cmd
	m.endpointInputs[m.endpointInputIndex], command = m.endpointInputs[m.endpointInputIndex].Update(key)
	return m, command
}

func (m Model) startEndpointForm(endpoint endpointconfig.Endpoint) (tea.Model, tea.Cmd) {
	m.endpointInputs = make([]textinput.Model, len(endpointInputLabels))
	_, inputWidth := m.endpointFormWidths()
	for index, label := range endpointInputLabels {
		input := newInput(label)
		input.SetWidth(inputWidth)
		m.endpointInputs[index] = input
	}
	m.endpointInputs[endpointAPIKeyInput].EchoMode = textinput.EchoPassword
	m.endpointInputs[endpointAPIKeyInput].EchoCharacter = '•'
	m.endpointInputs[endpointAPIKeyInput].Placeholder = "Stored separately with mode 0600"
	m.endpointInputs[endpointAuthHeaderInput].SetValue("Authorization")
	m.endpointInputs[endpointAuthSchemeInput].SetValue("Bearer")
	m.endpointInputs[endpointTimeoutInput].SetValue("30s")
	m.endpointInputs[endpointRetriesInput].SetValue("2")
	m.editingEndpointID = endpoint.ID
	if endpoint.ID != "" {
		m.endpointInputs[endpointNameInput].SetValue(endpoint.Name)
		m.endpointInputs[endpointURLInput].SetValue(endpoint.URL)
		m.endpointInputs[endpointAPIKeyInput].Placeholder = "Leave blank to keep the saved key"
		m.endpointInputs[endpointAuthHeaderInput].SetValue(endpoint.AuthHeader)
		m.endpointInputs[endpointAuthSchemeInput].SetValue(endpoint.AuthScheme)
		m.endpointInputs[endpointTimeoutInput].SetValue(endpoint.Timeout)
		m.endpointInputs[endpointRetriesInput].SetValue(strconv.Itoa(endpoint.ReadRetries))
	}
	m.endpointInputIndex = 0
	m.errorText = ""
	m.status = "Settings are stored under " + m.endpointStore.Directory()
	m.screen = screenEndpointCreate
	return m, m.endpointInputs[0].Focus()
}

func (m Model) endpointFormWidths() (int, int) {
	modalWidth := max(32, min(74, m.width-8))
	contentWidth := modalWidth - 8
	labelWidth := max(12, min(23, contentWidth-20))
	return labelWidth, max(8, contentWidth-labelWidth-1)
}

func (m Model) submitEndpoint() (tea.Model, tea.Cmd) {
	retries, err := strconv.Atoi(strings.TrimSpace(m.endpointInputs[endpointRetriesInput].Value()))
	if err != nil || retries < 0 {
		m.errorText = "GET read retries must be a non-negative integer"
		return m, nil
	}
	endpoint := endpointconfig.Endpoint{
		ID:          m.editingEndpointID,
		Name:        strings.TrimSpace(m.endpointInputs[endpointNameInput].Value()),
		URL:         strings.TrimSpace(m.endpointInputs[endpointURLInput].Value()),
		AuthHeader:  strings.TrimSpace(m.endpointInputs[endpointAuthHeaderInput].Value()),
		AuthScheme:  strings.TrimSpace(m.endpointInputs[endpointAuthSchemeInput].Value()),
		Timeout:     strings.TrimSpace(m.endpointInputs[endpointTimeoutInput].Value()),
		ReadRetries: retries,
	}
	apiKey := strings.TrimSpace(m.endpointInputs[endpointAPIKeyInput].Value())
	if endpoint.Name == "" {
		m.errorText = "Endpoint name must not be empty"
		return m, nil
	}
	if endpoint.Timeout == "" {
		m.errorText = "Request timeout must not be empty"
		return m, nil
	}
	m.blurEndpointInputs()
	m.busy = true
	m.errorText = ""
	m.status = "Saving and connecting to " + endpoint.Name + "…"
	return m, tea.Batch(m.spinner.Tick, saveAndConnectEndpointCmd(m.endpointStore, endpoint, apiKey))
}

func (m Model) focusEndpointInput(delta int) (tea.Model, tea.Cmd) {
	m.endpointInputs[m.endpointInputIndex].Blur()
	m.endpointInputIndex = (m.endpointInputIndex + delta + len(m.endpointInputs)) % len(m.endpointInputs)
	return m, m.endpointInputs[m.endpointInputIndex].Focus()
}

func (m *Model) blurEndpointInputs() {
	for index := range m.endpointInputs {
		m.endpointInputs[index].Blur()
	}
}

func (m Model) startEndpointSelection() (tea.Model, tea.Cmd) {
	if m.endpointStore == nil {
		return m, nil
	}
	m.endpointReturn = m.screen
	m.endpointIndex = 0
	m.errorText = ""
	m.status = fmt.Sprintf("Configuration: %s", m.endpointStore.ConfigPath())
	m.screen = screenEndpointSelect
	return m, nil
}

func (m Model) handleEndpointConnected(msg endpointConnectedMsg) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.errorText = msg.err.Error()
		m.status = "Could not configure the SCIM endpoint"
		m.screen = msg.failure
		if msg.failure == screenEndpointCreate && len(m.endpointInputs) > 0 {
			return m, m.endpointInputs[m.endpointInputIndex].Focus()
		}
		return m, nil
	}
	m.client = msg.client
	m.endpointName = msg.endpoint.Name
	m.endpoint = msg.client.Endpoint()
	m.upsertEndpoint(msg.endpoint)
	m.users = nil
	m.groups = nil
	m.visible = nil
	m.resetListControls()
	m.activeType = "Users"
	m.table.SetRows(nil)
	m.table.SetCursor(0)
	m.endpointInputs = nil
	m.editingEndpointID = ""
	m.errorText = ""
	m.pendingLoad = 2
	m.busy = true
	m.status = "Connecting to " + msg.endpoint.Name + "…"
	m.screen = screenBrowse
	return m, tea.Batch(
		m.spinner.Tick,
		loadResourcesCmd(m.ctx, m.client, "Users"),
		loadResourcesCmd(m.ctx, m.client, "Groups"),
	)
}

func (m *Model) upsertEndpoint(endpoint endpointconfig.Endpoint) {
	for index := range m.endpoints {
		if m.endpoints[index].ID == endpoint.ID {
			m.endpoints[index] = endpoint
			return
		}
	}
	m.endpoints = append(m.endpoints, endpoint)
}

func connectEndpointCmd(store *endpointconfig.Store, endpoint endpointconfig.Endpoint, failure screen) tea.Cmd {
	return func() tea.Msg {
		apiKey, err := store.APIKey(endpoint)
		if err != nil {
			return endpointConnectedMsg{endpoint: endpoint, failure: failure, err: err}
		}
		client, err := clientForEndpoint(endpoint, apiKey)
		return endpointConnectedMsg{endpoint: endpoint, client: client, failure: failure, err: err}
	}
}

func saveAndConnectEndpointCmd(store *endpointconfig.Store, endpoint endpointconfig.Endpoint, apiKey string) tea.Cmd {
	return func() tea.Msg {
		clientKey := apiKey
		if clientKey == "" && endpoint.ID != "" {
			var err error
			clientKey, err = store.APIKey(endpoint)
			if err != nil {
				return endpointConnectedMsg{endpoint: endpoint, failure: screenEndpointCreate, err: err}
			}
		}
		client, err := clientForEndpoint(endpoint, clientKey)
		if err != nil {
			return endpointConnectedMsg{endpoint: endpoint, failure: screenEndpointCreate, err: err}
		}
		saved, err := store.Save(endpoint, apiKey)
		if err != nil {
			return endpointConnectedMsg{endpoint: endpoint, failure: screenEndpointCreate, err: err}
		}
		return endpointConnectedMsg{endpoint: saved, client: client, failure: screenEndpointCreate}
	}
}

func clientForEndpoint(endpoint endpointconfig.Endpoint, apiKey string) (*scim.Client, error) {
	timeout, err := time.ParseDuration(endpoint.Timeout)
	if err != nil || timeout <= 0 {
		return nil, errors.New("request timeout must be a positive duration such as 30s or 1m")
	}
	return scim.NewClient(
		endpoint.URL,
		apiKey,
		endpoint.AuthHeader,
		endpoint.AuthScheme,
		timeout,
		scim.WithMaxReadRetries(endpoint.ReadRetries),
	)
}

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	endpointconfig "pool-skimmer/internal/config"
)

func TestPathValueParsesJSONAndPlainStrings(t *testing.T) {
	tests := []struct {
		input string
		want  any
	}{
		{"active=false", false},
		{`displayName="New Name"`, "New Name"},
		{"displayName=New Name", "New Name"},
		{`members=[{"value":"1","type":"User"}]`, []any{map[string]any{"value": "1", "type": "User"}}},
	}
	for _, test := range tests {
		_, got, err := pathValue(test.input)
		if err != nil {
			t.Fatalf("pathValue(%q): %v", test.input, err)
		}
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(test.want)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("pathValue(%q) = %s, want %s", test.input, gotJSON, wantJSON)
		}
	}
}

func TestGroupRenameDryRunNeedsNoCredentials(t *testing.T) {
	output, _, err := executeCommand(t, nil, "groups", "rename", "group-1", "Platform IT", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, `"path": "displayName"`) || !strings.Contains(output, `"value": "Platform IT"`) {
		t.Fatalf("output = %s", output)
	}
}

func TestPatchPreservesCommasInsideJSONValues(t *testing.T) {
	output, _, err := executeCommand(
		t, nil, "groups", "patch", "group-1", "--dry-run",
		"--add", `members=[{"value":"user-1","type":"User"}]`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, `"type": "User"`) || !strings.Contains(output, `"value": "user-1"`) {
		t.Fatalf("output = %s", output)
	}
}

func TestRenamePatchesThenReadsBack(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		switch requests {
		case 1:
			if request.Method != http.MethodPatch || request.URL.EscapedPath() != "/Groups/group-1" {
				t.Fatalf("request = %s %s", request.Method, request.URL.EscapedPath())
			}
			writer.WriteHeader(http.StatusNoContent)
		case 2:
			if request.Method != http.MethodGet {
				t.Fatalf("method = %s", request.Method)
			}
			io.WriteString(writer, `{"id":"group-1","displayName":"Platform IT"}`)
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	output, _, err := executeCommand(
		t, nil, "--endpoint", server.URL, "--api-key", "secret",
		"groups", "rename", "group-1", "Platform IT",
	)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || !strings.Contains(output, `"displayName": "Platform IT"`) {
		t.Fatalf("requests=%d output=%s", requests, output)
	}
}

func TestListRendersSafeTableAndPaginationHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		io.WriteString(writer, `{"Resources":[{"id":"1","displayName":"unsafe\u001b[31mname","members":[]}],"totalResults":2}`)
	}))
	defer server.Close()

	output, errorsOutput, err := executeCommand(
		t, nil, "--endpoint", server.URL, "--api-key", "secret", "groups", "list", "--count", "1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "\x1b") || !strings.Contains(output, "unsafe") || !strings.Contains(output, "name") {
		t.Fatalf("unsafe table output = %q", output)
	}
	if !strings.Contains(errorsOutput, "Showing 1 of 2") {
		t.Fatalf("stderr = %q", errorsOutput)
	}
}

func TestResourceExportWritesAllPagesAsSecureJSONArray(t *testing.T) {
	for _, resourceType := range []string{"Users", "Groups"} {
		t.Run(resourceType, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests++
				if request.Method != http.MethodGet || request.URL.Path != "/"+resourceType {
					t.Fatalf("request = %s %s", request.Method, request.URL.Path)
				}
				if request.URL.Query().Get("count") != "1" || request.URL.Query().Get("filter") != "externalId pr" {
					t.Fatalf("query = %s", request.URL.RawQuery)
				}
				if request.URL.Query().Get("attributes") != "id,externalId" {
					t.Fatalf("attributes = %q", request.URL.Query().Get("attributes"))
				}
				switch request.URL.Query().Get("startIndex") {
				case "1":
					io.WriteString(writer, `{"Resources":[{"id":"one","externalId":"first"}],"totalResults":2,"startIndex":1,"itemsPerPage":1}`)
				case "2":
					io.WriteString(writer, `{"Resources":[{"id":"two","externalId":"second"}],"totalResults":2,"startIndex":2,"itemsPerPage":1}`)
				default:
					t.Fatalf("startIndex = %q", request.URL.Query().Get("startIndex"))
				}
			}))
			defer server.Close()

			path := filepath.Join(t.TempDir(), strings.ToLower(resourceType)+".json")
			output, _, err := executeCommand(
				t, nil, "--endpoint", server.URL, "--api-key", "secret",
				strings.ToLower(resourceType), "export", path, "--count", "1",
				"--filter", "externalId pr", "--attributes", "id,externalId",
			)
			if err != nil {
				t.Fatal(err)
			}
			if requests != 2 || !strings.Contains(output, "Exported 2 "+strings.ToLower(resourceType)) {
				t.Fatalf("requests=%d output=%q", requests, output)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var resources []map[string]any
			if err := json.Unmarshal(data, &resources); err != nil {
				t.Fatalf("decode export: %v\n%s", err, data)
			}
			if len(resources) != 2 || resources[0]["id"] != "one" || resources[1]["id"] != "two" {
				t.Fatalf("resources = %#v", resources)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("export permissions = %o", info.Mode().Perm())
			}
		})
	}
}

func TestResourceExportRefusesExistingFileBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		io.WriteString(writer, `{"Resources":[],"totalResults":0}`)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "users.json")
	if err := os.WriteFile(path, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := executeCommand(
		t, nil, "--endpoint", server.URL, "--api-key", "secret", "users", "export", path,
	)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d", requests)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || string(data) != "keep me" {
		t.Fatalf("file = %q, error = %v", data, readErr)
	}

	output, _, err := executeCommand(
		t, nil, "--endpoint", server.URL, "--api-key", "secret", "users", "export", path, "--force",
	)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !strings.Contains(output, "Exported 0 users") {
		t.Fatalf("requests=%d output=%q", requests, output)
	}
	data, readErr = os.ReadFile(path)
	if readErr != nil || strings.TrimSpace(string(data)) != "[]" {
		t.Fatalf("file = %q, error = %v", data, readErr)
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
}

func TestNonInteractiveDeleteRequiresYes(t *testing.T) {
	_, _, err := executeCommand(t, strings.NewReader("yes\n"), "users", "delete", "user-1")
	if err == nil || !strings.Contains(err.Error(), "non-interactive delete") {
		t.Fatalf("error = %v", err)
	}
}

func TestNoArgumentsRequiresInteractiveTerminal(t *testing.T) {
	_, _, err := executeCommand(t, nil)
	if err == nil || !strings.Contains(err.Error(), "interactive TUI requires a terminal") {
		t.Fatalf("error = %v", err)
	}
}

func TestInvalidTimeoutEnvironmentIsReported(t *testing.T) {
	t.Setenv("SCIM_ENDPOINT", "https://example.test/scim/v2")
	t.Setenv("SCIM_API_KEY", "secret")
	t.Setenv("SCIM_TIMEOUT", "not-a-duration")
	_, _, err := executeCommand(t, nil, "users", "list")
	if err == nil || !strings.Contains(err.Error(), "invalid timeout") {
		t.Fatalf("error = %v", err)
	}
}

func TestCustomAuthenticationHeaderWithoutScheme(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-API-Key") != "secret" {
			t.Fatalf("X-API-Key = %q", request.Header.Get("X-API-Key"))
		}
		io.WriteString(writer, `{"Resources":[],"totalResults":0}`)
	}))
	defer server.Close()

	_, _, err := executeCommand(
		t, nil, "--endpoint", server.URL, "--api-key", "secret",
		"--auth-header", "X-API-Key", "--auth-scheme", "", "users", "list",
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestNamedProfileConfiguresNonInteractiveCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer profile-secret" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		io.WriteString(writer, `{"Resources":[],"totalResults":0}`)
	}))
	defer server.Close()

	directory := filepath.Join(t.TempDir(), endpointconfig.DirectoryName)
	store := endpointconfig.NewStore(directory)
	_, err := store.Save(endpointconfig.Endpoint{
		Name: "Production", URL: server.URL, AuthHeader: "Authorization", AuthScheme: "Bearer",
		Timeout: "30s", ReadRetries: 2,
	}, "profile-secret")
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = executeCommand(t, nil, "--config-dir", directory, "--profile", "production", "users", "list")
	if err != nil {
		t.Fatal(err)
	}
}

func TestProfileKeyIsNotReusedForOverriddenEndpoint(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		io.WriteString(writer, `{"Resources":[],"totalResults":0}`)
	}))
	defer server.Close()

	directory := filepath.Join(t.TempDir(), endpointconfig.DirectoryName)
	store := endpointconfig.NewStore(directory)
	_, err := store.Save(endpointconfig.Endpoint{
		Name: "Production", URL: "https://example.test/scim/v2", AuthHeader: "Authorization", AuthScheme: "Bearer",
		Timeout: "30s", ReadRetries: 2,
	}, "profile-secret")
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = executeCommand(t, nil, "--config-dir", directory, "--profile", "Production", "--endpoint", server.URL, "users", "list")
	if err == nil || !strings.Contains(err.Error(), "saved key") {
		t.Fatalf("error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("overridden endpoint received %d requests", requests)
	}
}

func executeCommand(t *testing.T, input io.Reader, arguments ...string) (string, string, error) {
	t.Helper()
	command := NewRootCommand("test")
	var output, errorsOutput bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&errorsOutput)
	if input != nil {
		command.SetIn(input)
	}
	command.SetArgs(arguments)
	err := command.Execute()
	return output.String(), errorsOutput.String(), err
}

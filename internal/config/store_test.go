package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreSavesNamedEndpointsAndSeparateCredentials(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), DirectoryName))
	first, err := store.Save(testEndpoint("Production", "https://example.test/scim/v2"), "production-secret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Save(testEndpoint("Staging", "https://staging.example.test/scim/v2"), "staging-secret")
	if err != nil {
		t.Fatal(err)
	}

	endpoints, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 2 || endpoints[0].Name != "Production" || endpoints[1].Name != "Staging" {
		t.Fatalf("endpoints = %#v", endpoints)
	}
	configContents, err := os.ReadFile(store.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configContents), "production-secret") || strings.Contains(string(configContents), "staging-secret") {
		t.Fatal("config.json contains an API key")
	}
	if key, err := store.APIKey(first); err != nil || key != "production-secret" {
		t.Fatalf("first key = %q, err = %v", key, err)
	}
	if key, err := store.APIKey(second); err != nil || key != "staging-secret" {
		t.Fatalf("second key = %q, err = %v", key, err)
	}

	assertMode(t, store.Directory(), 0o700)
	assertMode(t, filepath.Join(store.Directory(), CredentialsDir), 0o700)
	assertMode(t, store.ConfigPath(), 0o600)
	assertMode(t, store.credentialPath(first.ID), 0o600)
}

func TestStoreUpdatesSettingsAndPreservesCredential(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), DirectoryName))
	endpoint, err := store.Save(testEndpoint("Production", "https://example.test/scim/v2"), "secret")
	if err != nil {
		t.Fatal(err)
	}
	endpoint.Name = "Primary"
	endpoint.Timeout = "45s"
	updated, err := store.Save(endpoint, "")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != endpoint.ID || updated.Name != "Primary" || updated.Timeout != "45s" {
		t.Fatalf("updated = %#v", updated)
	}
	if key, err := store.APIKey(updated); err != nil || key != "secret" {
		t.Fatalf("key = %q, err = %v", key, err)
	}
}

func TestStoreRejectsDuplicateNamesAndDoesNotExposeKeysInErrors(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), DirectoryName))
	if _, err := store.Save(testEndpoint("Production", "https://example.test/scim/v2"), "secret-one"); err != nil {
		t.Fatal(err)
	}
	_, err := store.Save(testEndpoint("production", "https://other.example.test/scim/v2"), "secret-two")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "secret-one") || strings.Contains(err.Error(), "secret-two") {
		t.Fatalf("error exposed a credential: %v", err)
	}
}

func TestStoreRejectsUnknownFieldsIncludingInlineAPIKeys(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), DirectoryName))
	if err := os.MkdirAll(store.Directory(), 0o700); err != nil {
		t.Fatal(err)
	}
	document := `{"version":1,"endpoints":[{"id":"0123456789abcdef0123456789abcdef","name":"Production","endpoint":"https://example.test/scim/v2","auth_header":"Authorization","auth_scheme":"Bearer","timeout":"30s","read_retries":2,"api_key":"never-expose-this"}]}`
	if err := os.WriteFile(store.ConfigPath(), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := store.Load()
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "never-expose-this") {
		t.Fatalf("error exposed the inline key: %v", err)
	}
}

func testEndpoint(name, endpoint string) Endpoint {
	return Endpoint{
		Name: name, URL: endpoint, AuthHeader: "Authorization", AuthScheme: "Bearer",
		Timeout: "30s", ReadRetries: 2,
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

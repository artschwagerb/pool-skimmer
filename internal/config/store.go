// Package config stores named SCIM endpoint profiles and their credentials.
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	DirectoryName    = ".pool-skimmer"
	ConfigFileName   = "config.json"
	CredentialsDir   = "credentials"
	currentVersion   = 1
	maxCredentialLen = 64 << 10
)

// Endpoint is a named SCIM service configuration. API keys are stored in a
// separate permission-restricted file identified by ID.
type Endpoint struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"endpoint"`
	AuthHeader  string `json:"auth_header"`
	AuthScheme  string `json:"auth_scheme"`
	Timeout     string `json:"timeout"`
	ReadRetries int    `json:"read_retries"`
}

type fileConfig struct {
	Version   int        `json:"version"`
	Endpoints []Endpoint `json:"endpoints"`
}

// Store reads and writes a pool-skimmer configuration directory.
type Store struct {
	directory string
}

// DefaultStore returns the store rooted at ~/.pool-skimmer.
func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, errors.New("could not determine the home directory for ~/.pool-skimmer")
	}
	return NewStore(filepath.Join(home, DirectoryName)), nil
}

// NewStore returns a store rooted at directory.
func NewStore(directory string) *Store {
	return &Store{directory: filepath.Clean(directory)}
}

// Directory returns the configuration directory.
func (s *Store) Directory() string {
	return s.directory
}

// ConfigPath returns the configuration file path.
func (s *Store) ConfigPath() string {
	return filepath.Join(s.directory, ConfigFileName)
}

// Load returns every configured endpoint in file order. A missing file is an
// empty configuration.
func (s *Store) Load() ([]Endpoint, error) {
	contents, err := os.ReadFile(s.ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return []Endpoint{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", s.ConfigPath(), err)
	}
	var document fileConfig
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("could not parse %s: %w", s.ConfigPath(), err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("configuration contains multiple JSON values")
		}
		return nil, fmt.Errorf("could not parse %s: %w", s.ConfigPath(), err)
	}
	if document.Version != currentVersion {
		return nil, fmt.Errorf("unsupported pool-skimmer configuration version %d", document.Version)
	}
	seenIDs := make(map[string]bool, len(document.Endpoints))
	seenNames := make(map[string]bool, len(document.Endpoints))
	for _, endpoint := range document.Endpoints {
		if err := validateEndpoint(endpoint); err != nil {
			return nil, fmt.Errorf("invalid endpoint configuration: %w", err)
		}
		nameKey := strings.ToLower(endpoint.Name)
		if seenIDs[endpoint.ID] || seenNames[nameKey] {
			return nil, errors.New("endpoint names and IDs must be unique")
		}
		seenIDs[endpoint.ID] = true
		seenNames[nameKey] = true
	}
	return append([]Endpoint(nil), document.Endpoints...), nil
}

// Find returns a named endpoint using a case-insensitive name match.
func (s *Store) Find(name string) (Endpoint, error) {
	endpoints, err := s.Load()
	if err != nil {
		return Endpoint{}, err
	}
	for _, endpoint := range endpoints {
		if strings.EqualFold(endpoint.Name, strings.TrimSpace(name)) {
			return endpoint, nil
		}
	}
	return Endpoint{}, fmt.Errorf("SCIM endpoint profile %q was not found in %s", name, s.ConfigPath())
}

// Save creates or updates an endpoint. A new endpoint requires an API key. An
// empty API key preserves the credential for an existing endpoint.
func (s *Store) Save(endpoint Endpoint, apiKey string) (Endpoint, error) {
	endpoint.Name = strings.TrimSpace(endpoint.Name)
	endpoint.URL = strings.TrimSpace(endpoint.URL)
	endpoint.AuthHeader = strings.TrimSpace(endpoint.AuthHeader)
	endpoint.Timeout = strings.TrimSpace(endpoint.Timeout)
	apiKey = strings.TrimSpace(apiKey)

	endpoints, err := s.Load()
	if err != nil {
		return Endpoint{}, err
	}
	if endpoint.ID == "" {
		if apiKey == "" {
			return Endpoint{}, errors.New("SCIM API key must not be empty")
		}
		endpoint.ID, err = newEndpointID()
		if err != nil {
			return Endpoint{}, err
		}
	}
	if err := validateEndpoint(endpoint); err != nil {
		return Endpoint{}, err
	}

	match := -1
	for index, existing := range endpoints {
		if existing.ID == endpoint.ID {
			match = index
			continue
		}
		if strings.EqualFold(existing.Name, endpoint.Name) {
			return Endpoint{}, fmt.Errorf("a SCIM endpoint named %q already exists", endpoint.Name)
		}
	}
	if match < 0 && apiKey == "" {
		return Endpoint{}, errors.New("SCIM API key must not be empty")
	}
	if match >= 0 && apiKey == "" {
		if _, err := s.APIKey(endpoint); err != nil {
			return Endpoint{}, errors.New("enter an API key because the saved credential is unavailable")
		}
	}

	if err := s.ensureDirectories(); err != nil {
		return Endpoint{}, err
	}
	if apiKey != "" {
		if strings.ContainsAny(apiKey, "\r\n") {
			return Endpoint{}, errors.New("SCIM API key must not contain newlines")
		}
		if err := atomicWrite(s.credentialPath(endpoint.ID), []byte(apiKey+"\n"), 0o600); err != nil {
			return Endpoint{}, errors.New("could not save the SCIM API key")
		}
	}
	if match >= 0 {
		endpoints[match] = endpoint
	} else {
		endpoints = append(endpoints, endpoint)
	}
	document, err := json.MarshalIndent(fileConfig{Version: currentVersion, Endpoints: endpoints}, "", "  ")
	if err != nil {
		return Endpoint{}, fmt.Errorf("could not encode endpoint configuration: %w", err)
	}
	document = append(document, '\n')
	if err := atomicWrite(s.ConfigPath(), document, 0o600); err != nil {
		return Endpoint{}, fmt.Errorf("could not save %s: %w", s.ConfigPath(), err)
	}
	return endpoint, nil
}

// APIKey reads the saved credential without returning it in an error.
func (s *Store) APIKey(endpoint Endpoint) (string, error) {
	file, err := os.Open(s.credentialPath(endpoint.ID))
	if err != nil {
		return "", errors.New("could not read the saved SCIM API key")
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxCredentialLen+1))
	if err != nil || len(contents) > maxCredentialLen {
		return "", errors.New("could not read the saved SCIM API key")
	}
	apiKey := strings.TrimSpace(string(contents))
	if apiKey == "" || strings.ContainsAny(apiKey, "\r\n") {
		return "", errors.New("the saved SCIM API key is invalid")
	}
	return apiKey, nil
}

func (s *Store) ensureDirectories() error {
	if err := os.MkdirAll(s.directory, 0o700); err != nil {
		return fmt.Errorf("could not create %s: %w", s.directory, err)
	}
	if err := os.Chmod(s.directory, 0o700); err != nil {
		return fmt.Errorf("could not protect %s: %w", s.directory, err)
	}
	credentials := filepath.Join(s.directory, CredentialsDir)
	if err := os.MkdirAll(credentials, 0o700); err != nil {
		return fmt.Errorf("could not create the credentials directory: %w", err)
	}
	if err := os.Chmod(credentials, 0o700); err != nil {
		return fmt.Errorf("could not protect the credentials directory: %w", err)
	}
	return nil
}

func (s *Store) credentialPath(id string) string {
	return filepath.Join(s.directory, CredentialsDir, id+".key")
}

func validateEndpoint(endpoint Endpoint) error {
	if len(endpoint.ID) != 32 {
		return errors.New("endpoint ID is invalid")
	}
	if _, err := hex.DecodeString(endpoint.ID); err != nil {
		return errors.New("endpoint ID is invalid")
	}
	if endpoint.Name == "" || strings.ContainsAny(endpoint.Name, "\r\n") {
		return errors.New("endpoint name must not be empty or contain newlines")
	}
	if endpoint.URL == "" {
		return errors.New("SCIM endpoint must not be empty")
	}
	if endpoint.AuthHeader == "" || strings.ContainsAny(endpoint.AuthHeader, "\r\n") {
		return errors.New("authentication header must not be empty or contain newlines")
	}
	if strings.ContainsAny(endpoint.AuthScheme, "\r\n") {
		return errors.New("authentication scheme must not contain newlines")
	}
	if endpoint.Timeout == "" {
		return errors.New("timeout must not be empty")
	}
	if endpoint.ReadRetries < 0 {
		return errors.New("read retries must not be negative")
	}
	return nil
}

func newEndpointID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", errors.New("could not create an endpoint identifier")
	}
	return hex.EncodeToString(value), nil
}

func atomicWrite(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".pool-skimmer-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

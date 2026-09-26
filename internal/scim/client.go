// Package scim provides a small provider-neutral SCIM 2.0 client.
package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	PatchOpSchema      = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	ListResponseSchema = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	maxResponseBytes   = 32 << 20
)

// Resource is a SCIM resource with provider-specific attributes preserved.
type Resource map[string]any

// ListOptions controls SCIM list requests.
type ListOptions struct {
	Filter             string
	StartIndex         int
	Count              int
	Attributes         []string
	ExcludedAttributes []string
}

// APIError is an error returned by a SCIM endpoint.
type APIError struct {
	StatusCode int
	SCIMType   string
	Detail     string
	RequestID  string
}

func (e *APIError) Error() string {
	parts := []string{fmt.Sprintf("HTTP %d", e.StatusCode)}
	if e.SCIMType != "" {
		parts = append(parts, e.SCIMType)
	}
	message := strings.Join(parts, " ") + ": " + e.Detail
	if e.RequestID != "" {
		message += " (request " + e.RequestID + ")"
	}
	return message
}

// Client is safe for concurrent use.
type Client struct {
	endpoint       *url.URL
	apiKey         string
	authHeader     string
	authScheme     string
	httpClient     *http.Client
	maxReadRetries int
	sleep          func(context.Context, time.Duration) error
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client. It is primarily useful for tests.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithMaxReadRetries controls retries for GET requests. Writes are never retried.
func WithMaxReadRetries(retries int) Option {
	return func(c *Client) {
		if retries >= 0 {
			c.maxReadRetries = retries
		}
	}
}

// WithSleep replaces retry sleeping. It is primarily useful for tests.
func WithSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(c *Client) {
		if sleep != nil {
			c.sleep = sleep
		}
	}
}

// NewClient creates a SCIM client.
func NewClient(
	endpoint string,
	apiKey string,
	authHeader string,
	authScheme string,
	timeout time.Duration,
	options ...Option,
) (*Client, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("SCIM endpoint must be an absolute http:// or https:// URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("SCIM endpoint must use http:// or https://")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("SCIM endpoint must not contain a query string or fragment")
	}
	if parsed.User != nil {
		return nil, errors.New("SCIM endpoint must not contain embedded credentials")
	}
	if apiKey == "" {
		return nil, errors.New("SCIM API key must not be empty")
	}
	if timeout <= 0 {
		return nil, errors.New("timeout must be greater than zero")
	}
	if authHeader == "" || containsNewline(authHeader) {
		return nil, errors.New("authentication header must not be empty or contain newlines")
	}
	if containsNewline(apiKey) || containsNewline(authScheme) {
		return nil, errors.New("authentication values must not contain newlines")
	}

	client := &Client{
		endpoint:       parsed,
		apiKey:         apiKey,
		authHeader:     authHeader,
		authScheme:     strings.TrimSpace(authScheme),
		httpClient:     &http.Client{Timeout: timeout},
		maxReadRetries: 2,
		sleep:          sleepContext,
	}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

// Endpoint returns the configured service root without credentials.
func (c *Client) Endpoint() string {
	return c.endpoint.String()
}

// List retrieves one SCIM ListResponse page.
func (c *Client) List(ctx context.Context, resource string, options ListOptions) (map[string]any, error) {
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	if options.StartIndex == 0 {
		options.StartIndex = 1
	}
	if options.Count == 0 {
		options.Count = 100
	}
	if options.StartIndex < 1 {
		return nil, errors.New("start index must be at least 1")
	}
	if options.Count < 1 {
		return nil, errors.New("count must be at least 1")
	}

	query := url.Values{}
	query.Set("startIndex", strconv.Itoa(options.StartIndex))
	query.Set("count", strconv.Itoa(options.Count))
	if options.Filter != "" {
		query.Set("filter", options.Filter)
	}
	if len(options.Attributes) > 0 {
		query.Set("attributes", strings.Join(options.Attributes, ","))
	}
	if len(options.ExcludedAttributes) > 0 {
		query.Set("excludedAttributes", strings.Join(options.ExcludedAttributes, ","))
	}
	return c.request(ctx, http.MethodGet, resource, query, nil, "")
}

// ListAll follows SCIM's 1-based pagination and returns every remaining resource.
func (c *Client) ListAll(ctx context.Context, resource string, options ListOptions) ([]Resource, error) {
	if options.StartIndex == 0 {
		options.StartIndex = 1
	}
	if options.Count == 0 {
		options.Count = 100
	}
	nextIndex := options.StartIndex
	resources := make([]Resource, 0)
	for pageNumber := 0; pageNumber < 10000; pageNumber++ {
		options.StartIndex = nextIndex
		page, err := c.List(ctx, resource, options)
		if err != nil {
			return nil, err
		}
		pageResources, err := Resources(page)
		if err != nil {
			return nil, err
		}
		resources = append(resources, pageResources...)
		returned := len(pageResources)
		if returned == 0 {
			return resources, nil
		}

		pageStart := positiveInt(page["startIndex"], nextIndex)
		pageSize := positiveInt(page["itemsPerPage"], returned)
		candidate := pageStart + max(pageSize, returned)
		if candidate <= nextIndex {
			return nil, errors.New("SCIM endpoint returned non-progressing pagination")
		}
		nextIndex = candidate
		total := nonnegativeInt(page["totalResults"])
		if total >= 0 && nextIndex > total {
			return resources, nil
		}
		if total < 0 && returned < options.Count {
			return resources, nil
		}
	}
	return nil, errors.New("SCIM pagination exceeded 10,000 pages")
}

// Get retrieves a resource by ID.
func (c *Client) Get(ctx context.Context, resource, id string, attributes, excluded []string) (Resource, error) {
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	query := url.Values{}
	if len(attributes) > 0 {
		query.Set("attributes", strings.Join(attributes, ","))
	}
	if len(excluded) > 0 {
		query.Set("excludedAttributes", strings.Join(excluded, ","))
	}
	document, err := c.request(ctx, http.MethodGet, resource+"/"+url.PathEscape(id), query, nil, "")
	return Resource(document), err
}

// Create creates a SCIM resource.
func (c *Client) Create(ctx context.Context, resource string, document map[string]any) (Resource, error) {
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	response, err := c.request(ctx, http.MethodPost, resource, nil, document, "")
	return Resource(response), err
}

// Patch modifies a resource using a complete PatchOp document.
func (c *Client) Patch(ctx context.Context, resource, id string, document map[string]any, ifMatch string) (Resource, error) {
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	response, err := c.request(ctx, http.MethodPatch, resource+"/"+url.PathEscape(id), nil, document, ifMatch)
	return Resource(response), err
}

// Replace replaces a resource using PUT.
func (c *Client) Replace(ctx context.Context, resource, id string, document map[string]any, ifMatch string) (Resource, error) {
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	response, err := c.request(ctx, http.MethodPut, resource+"/"+url.PathEscape(id), nil, document, ifMatch)
	return Resource(response), err
}

// Delete deletes a SCIM resource.
func (c *Client) Delete(ctx context.Context, resource, id, ifMatch string) error {
	if err := validateResource(resource); err != nil {
		return err
	}
	_, err := c.request(ctx, http.MethodDelete, resource+"/"+url.PathEscape(id), nil, nil, ifMatch)
	return err
}

// Discovery retrieves one of SCIM's standard discovery resources.
func (c *Client) Discovery(ctx context.Context, name string) (map[string]any, error) {
	switch name {
	case "ServiceProviderConfig", "Schemas", "ResourceTypes":
		return c.request(ctx, http.MethodGet, name, nil, nil, "")
	default:
		return nil, fmt.Errorf("unsupported discovery resource %q", name)
	}
}

// Resources extracts and validates Resources from a ListResponse.
func Resources(document map[string]any) ([]Resource, error) {
	raw, ok := document["Resources"]
	if !ok {
		return []Resource{}, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("SCIM ListResponse.Resources was not an array")
	}
	resources := make([]Resource, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("SCIM ListResponse contained a non-object resource")
		}
		resources = append(resources, Resource(object))
	}
	return resources, nil
}

func (c *Client) request(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	payload any,
	ifMatch string,
) (map[string]any, error) {
	requestURL := strings.TrimRight(c.endpoint.String(), "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}

	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}

	attempts := 1
	if method == http.MethodGet {
		attempts += c.maxReadRetries
	}
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Accept", "application/scim+json, application/json")
		authValue := c.apiKey
		if c.authScheme != "" {
			authValue = c.authScheme + " " + c.apiKey
		}
		req.Header.Set(c.authHeader, authValue)
		if payload != nil {
			req.Header.Set("Content-Type", "application/scim+json")
		}
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}

		response, requestErr := c.httpClient.Do(req)
		if requestErr != nil {
			if method == http.MethodGet && attempt+1 < attempts && ctx.Err() == nil {
				if err := c.sleep(ctx, backoff(attempt)); err != nil {
					return nil, err
				}
				continue
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("could not contact SCIM endpoint: %w", requestErr)
		}

		responseBody, readErr := readResponseBody(response.Body)
		response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			if method == http.MethodGet && attempt+1 < attempts && retryableStatus(response.StatusCode) {
				delay := retryAfter(response.Header.Get("Retry-After"), time.Now())
				if delay == 0 {
					delay = backoff(attempt)
				}
				if err := c.sleep(ctx, delay); err != nil {
					return nil, err
				}
				continue
			}
			return nil, apiError(response, responseBody)
		}
		if len(responseBody) == 0 {
			return nil, nil
		}
		return decodeObject(responseBody)
	}
	return nil, errors.New("request attempts exhausted")
}

func decodeObject(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return nil, errors.New("SCIM endpoint returned a non-object JSON response")
	}
	if document == nil {
		return nil, errors.New("SCIM endpoint returned a non-object JSON response")
	}
	return document, nil
}

func readResponseBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read SCIM response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("SCIM response exceeded %d bytes", maxResponseBytes)
	}
	return data, nil
}

func apiError(response *http.Response, body []byte) error {
	detail := http.StatusText(response.StatusCode)
	scimType := ""
	if document, err := decodeObject(body); err == nil {
		if value := stringValue(document["detail"]); value != "" {
			detail = value
		} else if value := stringValue(document["message"]); value != "" {
			detail = value
		} else if value := stringValue(document["errorSummary"]); value != "" {
			detail = value
		}
		scimType = stringValue(document["scimType"])
	}
	return &APIError{
		StatusCode: response.StatusCode,
		SCIMType:   scimType,
		Detail:     detail,
		RequestID:  requestID(response.Header),
	}
}

func requestID(header http.Header) string {
	for _, name := range []string{"X-Request-Id", "Request-Id", "Workos-Request-Id"} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, 30*time.Second)
	}
	if when, err := http.ParseTime(value); err == nil && when.After(now) {
		return min(when.Sub(now), 30*time.Second)
	}
	return 0
}

func backoff(attempt int) time.Duration {
	delay := 250 * time.Millisecond * time.Duration(1<<min(attempt, 6))
	return min(delay, 30*time.Second)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validateResource(resource string) error {
	if resource != "Users" && resource != "Groups" {
		return errors.New("resource must be Users or Groups")
	}
	return nil
}

func containsNewline(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func positiveInt(value any, fallback int) int {
	parsed := integer(value)
	if parsed > 0 {
		return parsed
	}
	return fallback
}

func nonnegativeInt(value any) int {
	parsed := integer(value)
	if parsed >= 0 {
		return parsed
	}
	return -1
}

func integer(value any) int {
	switch number := value.(type) {
	case json.Number:
		parsed, err := strconv.Atoi(number.String())
		if err == nil {
			return parsed
		}
	case float64:
		return int(number)
	case int:
		return number
	case string:
		parsed, err := strconv.Atoi(number)
		if err == nil {
			return parsed
		}
	}
	return -1
}

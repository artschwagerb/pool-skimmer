package scim

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListEncodesQueryAndAuthentication(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := request.URL.Query().Get("filter"); got != `userName eq "a+b@example.com"` {
			t.Fatalf("filter = %q", got)
		}
		if got := request.URL.Query().Get("attributes"); got != "id,userName" {
			t.Fatalf("attributes = %q", got)
		}
		writer.Header().Set("Content-Type", "application/scim+json")
		io.WriteString(writer, `{"Resources":[],"totalResults":0}`)
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	_, err := client.List(context.Background(), "Users", ListOptions{
		Filter:     `userName eq "a+b@example.com"`,
		Count:      25,
		Attributes: []string{"id", "userName"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestListAllFollowsPagination(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("startIndex") {
		case "1":
			io.WriteString(writer, `{"Resources":[{"id":"1"},{"id":"2"}],"startIndex":1,"itemsPerPage":2,"totalResults":3}`)
		case "3":
			io.WriteString(writer, `{"Resources":[{"id":"3"}],"startIndex":3,"itemsPerPage":1,"totalResults":3}`)
		default:
			t.Fatalf("unexpected startIndex %q", request.URL.Query().Get("startIndex"))
		}
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	resources, err := client.ListAll(context.Background(), "Users", ListOptions{Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 3 || resources[2]["id"] != "3" {
		t.Fatalf("resources = %#v", resources)
	}
}

func TestPatchBuildsHeadersAndEncodesID(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPatch {
			t.Fatalf("method = %s", request.Method)
		}
		if request.URL.EscapedPath() != "/Groups/a%2Fb" {
			t.Fatalf("path = %s", request.URL.EscapedPath())
		}
		if request.Header.Get("If-Match") != `W/"123"` {
			t.Fatalf("If-Match = %q", request.Header.Get("If-Match"))
		}
		if request.Header.Get("Content-Type") != "application/scim+json" {
			t.Fatalf("Content-Type = %q", request.Header.Get("Content-Type"))
		}
		var document map[string]any
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			t.Fatal(err)
		}
		if document["schemas"].([]any)[0] != PatchOpSchema {
			t.Fatalf("payload = %#v", document)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	_, err := client.Patch(context.Background(), "Groups", "a/b", map[string]any{
		"schemas": []string{PatchOpSchema},
		"Operations": []map[string]any{{
			"op": "replace", "path": "displayName", "value": "New Name",
		}},
	}, `W/"123"`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGETRetriesAndHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			writer.Header().Set("Retry-After", "1")
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(writer, `{"Resources":[],"totalResults":0}`)
	}))
	defer server.Close()

	var slept time.Duration
	client, err := NewClient(
		server.URL, "secret", "Authorization", "Bearer", time.Second,
		WithSleep(func(_ context.Context, duration time.Duration) error {
			slept = duration
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.List(context.Background(), "Groups", ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || slept != time.Second {
		t.Fatalf("attempts=%d slept=%s", attempts, slept)
	}
}

func TestWritesAreNotRetried(t *testing.T) {
	t.Parallel()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts++
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	_, err := client.Patch(context.Background(), "Users", "1", map[string]any{"schemas": []string{PatchOpSchema}}, "")
	if err == nil || attempts != 1 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}

func TestAPIErrorIncludesSCIMTypeAndRequestID(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Request-ID", "req-123")
		writer.WriteHeader(http.StatusNotFound)
		io.WriteString(writer, `{"detail":"Resource does not exist","scimType":"invalidValue"}`)
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	_, err := client.Get(context.Background(), "Users", "missing", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404 invalidValue: Resource does not exist (request req-123)") {
		t.Fatalf("error = %v", err)
	}
}

func TestInvalidEndpointRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewClient("example.test/scim", "secret", "Authorization", "Bearer", time.Second); err == nil {
		t.Fatal("expected invalid endpoint error")
	}
	if _, err := NewClient("https://example.test/scim?tenant=1", "secret", "Authorization", "Bearer", time.Second); err == nil {
		t.Fatal("expected query string error")
	}
}

func testClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := NewClient(endpoint, "secret", "Authorization", "Bearer", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/slackmgr/types"
)

func TestNew(t *testing.T) {
	t.Parallel()

	client := New("http://example.com", WithRetryCount(5))

	if client == nil {
		t.Fatal("expected client to be created")
	}

	if client.baseURL != "http://example.com" {
		t.Errorf("expected baseURL=http://example.com, got %s", client.baseURL)
	}

	if client.options.retryCount != 5 {
		t.Errorf("expected retryCount=5, got %d", client.options.retryCount)
	}
}

func TestConnect_EmptyURL(t *testing.T) {
	t.Parallel()

	client := New("")

	err := client.Connect(context.Background())

	if err == nil {
		t.Fatal("expected error for empty URL")
	}

	if err.Error() != "base URL must be set" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConnect_InvalidOptions(t *testing.T) {
	t.Parallel()

	client := New("http://example.com")
	// Force invalid options by setting nil logger
	client.options.requestLogger = nil

	err := client.Connect(context.Background())

	if err == nil {
		t.Fatal("expected error for invalid options")
	}

	if !strings.Contains(err.Error(), "invalid options") {
		t.Errorf("expected error to contain 'invalid options', got: %v", err)
	}
}

func TestConnect_PingFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := New(server.URL, WithRetryCount(0))

	err := client.Connect(context.Background())

	if err == nil {
		t.Fatal("expected error for ping failure")
	}

	if !strings.Contains(err.Error(), "failed to ping alerts API") {
		t.Errorf("expected error to contain 'failed to ping alerts API', got: %v", err)
	}

	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain '500', got: %v", err)
	}
}

func TestConnect_Success(t *testing.T) {
	t.Parallel()

	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)

	err := client.Connect(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if requestedPath != "/ping" {
		t.Errorf("expected path=/ping, got %s", requestedPath)
	}
}

func TestConnect_OnlyOnce(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)

	// First connect
	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("first connect failed: %v", err)
	}

	// Second connect should be no-op
	err = client.Connect(context.Background())
	if err != nil {
		t.Fatalf("second connect failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected ping to be called once, got %d", callCount)
	}
}

func TestConnect_SetsHeaders(t *testing.T) {
	t.Parallel()

	var contentType, accept, customHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		accept = r.Header.Get("Accept")
		customHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, WithRequestHeader("X-Custom", "custom-value"))

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if contentType != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %s", contentType)
	}

	if accept != "application/json" {
		t.Errorf("expected Accept=application/json, got %s", accept)
	}

	if customHeader != "custom-value" {
		t.Errorf("expected X-Custom=custom-value, got %s", customHeader)
	}
}

func TestConnect_SetsBasicAuth(t *testing.T) {
	t.Parallel()

	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, WithBasicAuth("user", "pass"))

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if !strings.HasPrefix(authHeader, "Basic ") {
		t.Errorf("expected Basic auth header, got %s", authHeader)
	}
}

func TestConnect_SetsTokenAuth(t *testing.T) {
	t.Parallel()

	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, WithAuthScheme("Bearer"), WithAuthToken("my-token"))

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if authHeader != "Bearer my-token" {
		t.Errorf("expected 'Bearer my-token', got %s", authHeader)
	}
}

func TestSend_NilClient(t *testing.T) {
	t.Parallel()

	var client *Client

	err := client.Send(context.Background(), &types.Alert{})

	if err == nil {
		t.Fatal("expected error for nil client")
	}

	if err.Error() != "alert client is nil" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSend_NotConnected(t *testing.T) {
	t.Parallel()

	client := New("http://example.com")

	err := client.Send(context.Background(), &types.Alert{})

	if err == nil {
		t.Fatal("expected error for not connected client")
	}

	if err.Error() != "client not connected - call Connect() first" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSend_EmptyAlerts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	err := client.Send(context.Background())

	if err == nil {
		t.Fatal("expected error for empty alerts")
	}

	if err.Error() != "alerts list cannot be empty" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSend_NilAlert(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	err := client.Send(context.Background(), &types.Alert{}, nil, &types.Alert{})

	if err == nil {
		t.Fatal("expected error for nil alert")
	}

	if err.Error() != "alert at index 1 is nil" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSend_Success(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	alert := &types.Alert{
		Header: "Test Alert",
	}
	err := client.Send(context.Background(), alert)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if capturedPath != "/alerts" {
		t.Errorf("expected path=/alerts, got %s", capturedPath)
	}

	if !strings.Contains(string(capturedBody), "Test Alert") {
		t.Errorf("expected body to contain 'Test Alert', got: %s", capturedBody)
	}
}

func TestSend_HTTPError_JSONErrorResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "validation failed: header is required"}`))
	}))
	defer server.Close()

	client := New(server.URL, WithRetryCount(0))
	_ = client.Connect(context.Background())

	err := client.Send(context.Background(), &types.Alert{})

	if err == nil {
		t.Fatal("expected error for HTTP error")
	}

	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to contain '400', got: %v", err)
	}

	// Should extract the error message from JSON
	if !strings.Contains(err.Error(), "validation failed: header is required") {
		t.Errorf("expected error to contain 'validation failed: header is required', got: %v", err)
	}
}

func TestSend_HTTPError_PlainTextResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad Request"))
	}))
	defer server.Close()

	client := New(server.URL, WithRetryCount(0))
	_ = client.Connect(context.Background())

	err := client.Send(context.Background(), &types.Alert{})

	if err == nil {
		t.Fatal("expected error for HTTP error")
	}

	// Should fall back to raw body for non-JSON response
	if !strings.Contains(err.Error(), "Bad Request") {
		t.Errorf("expected error to contain 'Bad Request', got: %v", err)
	}
}

func TestSend_HTTPError_JSONWithoutErrorField(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message": "something went wrong"}`))
	}))
	defer server.Close()

	client := New(server.URL, WithRetryCount(0))
	_ = client.Connect(context.Background())

	err := client.Send(context.Background(), &types.Alert{})

	if err == nil {
		t.Fatal("expected error for HTTP error")
	}

	// Should fall back to raw body when JSON doesn't have "error" field
	if !strings.Contains(err.Error(), `{"message": "something went wrong"}`) {
		t.Errorf("expected error to contain raw JSON body, got: %v", err)
	}
}

func TestSend_HTTPError_EmptyResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL, WithRetryCount(0))
	_ = client.Connect(context.Background())

	err := client.Send(context.Background(), &types.Alert{})

	if err == nil {
		t.Fatal("expected error for HTTP error")
	}

	if !strings.Contains(err.Error(), "(empty error body)") {
		t.Errorf("expected error to contain '(empty error body)', got: %v", err)
	}
}

func TestSend_RequestError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	client := New(server.URL, WithRetryCount(0))
	_ = client.Connect(context.Background())

	// Close server to cause connection error on Send
	server.Close()

	err := client.Send(context.Background(), &types.Alert{})

	if err == nil {
		t.Fatal("expected error for request failure")
	}

	if !strings.Contains(err.Error(), "POST") {
		t.Errorf("expected error to mention POST, got: %v", err)
	}
}

func TestConnect_RequestError(t *testing.T) {
	t.Parallel()

	// Use a URL that will fail to connect
	client := New("http://localhost:1", WithRetryCount(0))

	err := client.Connect(context.Background())

	if err == nil {
		t.Fatal("expected error for connection failure")
	}

	if !strings.Contains(err.Error(), "failed to ping alerts API") {
		t.Errorf("expected error to contain 'failed to ping alerts API', got: %v", err)
	}
}

func TestSend_MultipleAlerts(t *testing.T) {
	t.Parallel()

	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/alerts" {
			capturedBody, _ = io.ReadAll(r.Body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	alerts := []*types.Alert{
		{Header: "Alert 1"},
		{Header: "Alert 2"},
		{Header: "Alert 3"},
	}
	err := client.Send(context.Background(), alerts...)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, "Alert 1") ||
		!strings.Contains(bodyStr, "Alert 2") ||
		!strings.Contains(bodyStr, "Alert 3") {
		t.Errorf("expected body to contain all alerts, got: %s", bodyStr)
	}
}

func TestSend_JSONFormat(t *testing.T) {
	t.Parallel()

	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/alerts" {
			capturedBody, _ = io.ReadAll(r.Body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	alert := &types.Alert{
		Header: "Test Header",
		Text:   "Test Text",
	}
	err := client.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the JSON structure
	var result struct {
		Alerts []struct {
			Header string `json:"header"`
			Text   string `json:"text"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(capturedBody, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(result.Alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(result.Alerts))
	}

	if result.Alerts[0].Header != "Test Header" {
		t.Errorf("expected header='Test Header', got %s", result.Alerts[0].Header)
	}

	if result.Alerts[0].Text != "Test Text" {
		t.Errorf("expected text='Test Text', got %s", result.Alerts[0].Text)
	}
}

func TestConnect_RetryAfterFailure(t *testing.T) {
	t.Parallel()

	var healthy atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, WithRetryCount(0))

	if err := client.Connect(context.Background()); err == nil {
		t.Fatal("expected first connect to fail")
	}

	// A failed connect leaves the client unconnected.
	if client.RestyClient() != nil {
		t.Error("expected nil resty client after failed connect")
	}

	if err := client.Send(context.Background(), &types.Alert{}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected not connected error after failed connect, got %v", err)
	}

	if err := client.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected not connected error from Ping after failed connect, got %v", err)
	}

	healthy.Store(true)

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("expected second connect to succeed, got %v", err)
	}

	if err := client.Send(context.Background(), &types.Alert{}); err != nil {
		t.Errorf("expected send to succeed after connect, got %v", err)
	}
}

func TestConnect_ConcurrentWithSend(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	defer client.Close()

	var wg sync.WaitGroup

	for range 5 {
		wg.Go(func() {
			if err := client.Connect(context.Background()); err != nil {
				t.Errorf("connect failed: %v", err)
			}
		})
		wg.Go(func() {
			// Either not connected yet, or sent; the race detector checks the access to the connection.
			_ = client.Send(context.Background(), &types.Alert{})
			_ = client.RestyClient()
		})
	}

	wg.Wait()
}

func TestSend_ContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Simulate slow response
		<-r.Context().Done()
	}))
	defer server.Close()

	client := New(server.URL, WithRetryCount(0))
	_ = client.Connect(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.Send(ctx, &types.Alert{Header: "test"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}

	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

func TestSend_UnicodeContent(t *testing.T) {
	t.Parallel()

	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/alerts" {
			capturedBody, _ = io.ReadAll(r.Body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	// Test with various unicode characters (intentionally testing non-ASCII support)
	alert := &types.Alert{
		Header: "Alert: 日本語 🚨 émojis",    //nolint:gosmopolitan // testing unicode support
		Text:   "Здравствуй мир! 你好世界 🌍", //nolint:gosmopolitan // testing unicode support
	}
	err := client.Send(context.Background(), alert)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, "日本語") { //nolint:gosmopolitan // testing unicode support
		t.Errorf("expected body to contain Japanese, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "🚨") {
		t.Errorf("expected body to contain emoji, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Здравствуй") {
		t.Errorf("expected body to contain Russian, got: %s", bodyStr)
	}
}

func TestClient_Close(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	// Close should not panic
	client.Close()

	// Close on unconnected client should also not panic
	client2 := New(server.URL)
	client2.Close()
}

func TestClient_Ping(t *testing.T) {
	t.Parallel()

	pingCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			pingCount++
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	// Ping count is 1 from Connect
	if pingCount != 1 {
		t.Errorf("expected ping count 1 after connect, got %d", pingCount)
	}

	// Call Ping explicitly
	err := client.Ping(context.Background())
	if err != nil {
		t.Errorf("unexpected ping error: %v", err)
	}

	if pingCount != 2 {
		t.Errorf("expected ping count 2 after explicit ping, got %d", pingCount)
	}
}

func TestClient_Ping_NotConnected(t *testing.T) {
	t.Parallel()

	client := New("http://example.com")

	err := client.Ping(context.Background())

	if err == nil {
		t.Fatal("expected error for not connected client")
	}

	if err.Error() != "client not connected - call Connect() first" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_Ping_NilClient(t *testing.T) {
	t.Parallel()

	var client *Client

	err := client.Ping(context.Background())

	if err == nil {
		t.Fatal("expected error for nil client")
	}

	if err.Error() != "alert client is nil" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_RestyClient(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)

	// Before connect, should be nil
	if client.RestyClient() != nil {
		t.Error("expected nil resty client before connect")
	}

	_ = client.Connect(context.Background())

	// After connect, should not be nil
	if client.RestyClient() == nil {
		t.Error("expected non-nil resty client after connect")
	}
}

func TestConnect_CustomEndpoints(t *testing.T) {
	t.Parallel()

	var pingPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pingPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, WithPingEndpoint("health"))
	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pingPath != "/health" {
		t.Errorf("expected ping path=/health, got %s", pingPath)
	}
}

func TestSend_CustomAlertsEndpoint(t *testing.T) {
	t.Parallel()

	var alertsPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			alertsPath = r.URL.Path
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, WithAlertsEndpoint("v2/alerts"))
	_ = client.Connect(context.Background())

	err := client.Send(context.Background(), &types.Alert{Header: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if alertsPath != "/v2/alerts" {
		t.Errorf("expected alerts path=/v2/alerts, got %s", alertsPath)
	}
}

func TestConnect_SetsDefaultAuthScheme(t *testing.T) {
	t.Parallel()

	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Only set token, not scheme - should default to Bearer
	client := New(server.URL, WithAuthToken("my-token"))

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if authHeader != "Bearer my-token" {
		t.Errorf("expected 'Bearer my-token', got %s", authHeader)
	}
}

func TestSanitizeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no credentials",
			input:    "http://example.com/path",
			expected: "http://example.com/path",
		},
		{
			name:     "with credentials",
			input:    "http://user:password@example.com/path",
			expected: "http://***:***@example.com/path",
		},
		{
			name:     "invalid URL",
			input:    "://invalid",
			expected: "://invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := sanitizeURL(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestSendWithResponse_NilClient(t *testing.T) {
	t.Parallel()

	var c *Client

	meta, err := c.SendWithResponse(context.Background(), &types.Alert{})

	if err == nil {
		t.Fatal("expected error for nil client")
	}

	if err.Error() != "alert client is nil" {
		t.Errorf("unexpected error: %v", err)
	}

	if meta != nil {
		t.Error("expected nil metadata for nil client")
	}
}

func TestSendWithResponse_NotConnected(t *testing.T) {
	t.Parallel()

	c := New("http://example.com")

	meta, err := c.SendWithResponse(context.Background(), &types.Alert{})

	if err == nil {
		t.Fatal("expected error for not connected client")
	}

	if err.Error() != "client not connected - call Connect() first" {
		t.Errorf("unexpected error: %v", err)
	}

	if meta != nil {
		t.Error("expected nil metadata for not connected client")
	}
}

func TestSendWithResponse_EmptyAlerts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL)
	_ = c.Connect(context.Background())

	meta, err := c.SendWithResponse(context.Background())

	if err == nil {
		t.Fatal("expected error for empty alerts")
	}

	if err.Error() != "alerts list cannot be empty" {
		t.Errorf("unexpected error: %v", err)
	}

	if meta != nil {
		t.Error("expected nil metadata for empty alerts")
	}
}

func TestSendWithResponse_NilAlert(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL)
	_ = c.Connect(context.Background())

	meta, err := c.SendWithResponse(context.Background(), &types.Alert{}, nil)

	if err == nil {
		t.Fatal("expected error for nil alert")
	}

	if err.Error() != "alert at index 1 is nil" {
		t.Errorf("unexpected error: %v", err)
	}

	if meta != nil {
		t.Error("expected nil metadata for nil alert")
	}
}

func TestSendWithResponse_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("X-Request-ID", "abc123")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL)
	_ = c.Connect(context.Background())

	meta, err := c.SendWithResponse(context.Background(), &types.Alert{Header: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta == nil {
		t.Fatal("expected non-nil metadata on success")
	}

	if meta.StatusCode != http.StatusOK {
		t.Errorf("expected StatusCode=200, got %d", meta.StatusCode)
	}

	if meta.Duration <= 0 {
		t.Errorf("expected Duration > 0, got %v", meta.Duration)
	}

	if meta.Headers == nil {
		t.Fatal("expected non-nil Headers map")
	}

	if meta.Headers["X-Request-Id"] != "abc123" {
		t.Errorf("expected X-Request-Id=abc123, got %q", meta.Headers["X-Request-Id"])
	}
}

func TestSendWithResponse_HTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	c := New(server.URL, WithRetryCount(0))
	_ = c.Connect(context.Background())

	meta, err := c.SendWithResponse(context.Background(), &types.Alert{Header: "test"})

	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}

	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to contain '400', got: %v", err)
	}

	if meta == nil {
		t.Fatal("expected non-nil metadata even on HTTP error")
	}

	if meta.StatusCode != http.StatusBadRequest {
		t.Errorf("expected StatusCode=400, got %d", meta.StatusCode)
	}
}

func TestSendWithResponse_NetworkError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	c := New(server.URL, WithRetryCount(0))
	_ = c.Connect(context.Background())

	// Close server to trigger a network-level error
	server.Close()

	meta, err := c.SendWithResponse(context.Background(), &types.Alert{Header: "test"})

	if err == nil {
		t.Fatal("expected error for network failure")
	}

	if meta != nil {
		t.Errorf("expected nil metadata on network error, got %+v", meta)
	}
}

func TestSendAlert_DelegatesToSend(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	err := client.SendAlert(context.Background(), &types.Alert{Header: "Test Alert"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if capturedPath != "/alerts" {
		t.Errorf("expected path=/alerts, got %s", capturedPath)
	}

	if !strings.Contains(string(capturedBody), `"alerts":[{`) || !strings.Contains(string(capturedBody), "Test Alert") {
		t.Errorf("unexpected body: %s", capturedBody)
	}

	err = client.SendAlert(context.Background(), nil)
	if err == nil || err.Error() != "alert at index 0 is nil" {
		t.Errorf("unexpected error for nil alert: %v", err)
	}
}

func TestSendAlerts_DelegatesToSend(t *testing.T) {
	t.Parallel()

	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := New(server.URL)
	_ = client.Connect(context.Background())

	err := client.SendAlerts(context.Background(), []*types.Alert{{Header: "First"}, {Header: "Second"}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !strings.Contains(string(capturedBody), "First") || !strings.Contains(string(capturedBody), "Second") {
		t.Errorf("unexpected body: %s", capturedBody)
	}

	err = client.SendAlerts(context.Background(), nil)
	if err == nil || err.Error() != "alerts list cannot be empty" {
		t.Errorf("unexpected error for empty alerts: %v", err)
	}
}

func TestSend_WithAlertClient(t *testing.T) {
	t.Parallel()

	var captured alertsList

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/alerts" {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &captured)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, WithAlertClient("my-tool"))
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("unexpected connect error: %v", err)
	}

	withoutClient := &types.Alert{Header: "a"}
	blankClient := &types.Alert{Header: "b", Client: "  "}
	withClient := &types.Alert{Header: "c", Client: "prometheus"}

	if err := client.Send(context.Background(), withoutClient, blankClient, withClient); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(captured.Alerts) != 3 {
		t.Fatalf("expected 3 alerts, got %d", len(captured.Alerts))
	}

	for i, want := range []string{"my-tool", "my-tool", "prometheus"} {
		if got := captured.Alerts[i].Client; got != want {
			t.Errorf("alert %d: expected client=%q, got %q", i, want, got)
		}
	}

	if withoutClient.Client != "" || blankClient.Client != "  " || withClient.Client != "prometheus" {
		t.Error("the caller's alerts must not be modified")
	}
}

func TestSend_WithoutAlertClient(t *testing.T) {
	t.Parallel()

	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/alerts" {
			capturedBody, _ = io.ReadAll(r.Body)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("unexpected connect error: %v", err)
	}

	if err := client.Send(context.Background(), &types.Alert{Header: "a"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(string(capturedBody), `"client"`) {
		t.Errorf("expected no client in body, got: %s", capturedBody)
	}
}

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		valid   bool
	}{
		{"http", "http://localhost:8080", true},
		{"https with path", "https://api.example.com/v1/", true},
		{"empty", "", false},
		{"no scheme", "localhost:8080", false},
		{"relative", "/api", false},
		{"unsupported scheme", "ftp://example.com", false},
		{"no host", "http://", false},
		{"unparsable", "http://[::1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := validateBaseURL(tt.baseURL); (err == nil) != tt.valid {
				t.Errorf("expected valid=%v, got %v", tt.valid, err)
			}
		})
	}
}

func TestValidateBaseURL_RedactsCredentials(t *testing.T) {
	t.Parallel()

	err := New("ftp://user:secret@example.com").Connect(t.Context())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("expected an error without the password, got %v", err)
	}
}

func TestClient_NilReceiver(t *testing.T) {
	t.Parallel()

	var c *Client

	c.Close()

	if c.RestyClient() != nil {
		t.Error("expected nil resty client")
	}

	if err := c.Connect(t.Context()); err == nil {
		t.Error("expected error from Connect on nil client")
	}
}

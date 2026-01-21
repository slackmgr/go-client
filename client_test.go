package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	common "github.com/peteraglen/slack-manager-common"
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

	client := New(server.URL)

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

	err := client.Send(context.Background(), &common.Alert{})

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

	err := client.Send(context.Background(), &common.Alert{})

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

	err := client.Send(context.Background(), &common.Alert{}, nil, &common.Alert{})

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

	alert := &common.Alert{
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

	err := client.Send(context.Background(), &common.Alert{})

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

	err := client.Send(context.Background(), &common.Alert{})

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

	err := client.Send(context.Background(), &common.Alert{})

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

	err := client.Send(context.Background(), &common.Alert{})

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

	err := client.Send(context.Background(), &common.Alert{})

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

	alerts := []*common.Alert{
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

	alert := &common.Alert{
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

func TestConnect_ErrorPersistence(t *testing.T) {
	t.Parallel()

	// Use an invalid URL that will fail to connect
	client := New("http://localhost:1", WithRetryCount(0))

	// First connect should fail
	err1 := client.Connect(context.Background())
	if err1 == nil {
		t.Fatal("expected first connect to fail")
	}

	// Second connect should return the same error (not nil)
	err2 := client.Connect(context.Background())
	if err2 == nil {
		t.Fatal("expected second connect to return persisted error, got nil")
	}

	if err1.Error() != err2.Error() {
		t.Errorf("expected same error on second call, got %v vs %v", err1, err2)
	}
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

	err := client.Send(ctx, &common.Alert{Header: "test"})
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
	alert := &common.Alert{
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

	err := client.Send(context.Background(), &common.Alert{Header: "test"})
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

func TestParseRetryAfterHeader(t *testing.T) {
	t.Parallel()

	t.Run("empty header", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// No Retry-After header
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		resp := makeRestyRequest(t, server.URL)
		duration, err := parseRetryAfterHeader(nil, resp)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if duration != 0 {
			t.Errorf("expected 0 duration for empty header, got %v", duration)
		}
	})

	t.Run("seconds format", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		resp := makeRestyRequest(t, server.URL)
		duration, err := parseRetryAfterHeader(nil, resp)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if duration != 120*time.Second {
			t.Errorf("expected 120s, got %v", duration)
		}
	})

	t.Run("http-date format", func(t *testing.T) {
		t.Parallel()

		// Use a time in the future
		futureTime := time.Now().Add(60 * time.Second)
		httpDate := futureTime.UTC().Format(http.TimeFormat)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", httpDate)
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		resp := makeRestyRequest(t, server.URL)
		duration, err := parseRetryAfterHeader(nil, resp)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		// Allow some tolerance for test execution time
		if duration < 55*time.Second || duration > 65*time.Second {
			t.Errorf("expected ~60s, got %v", duration)
		}
	})

	t.Run("invalid format returns zero", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "not-a-valid-value")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		resp := makeRestyRequest(t, server.URL)
		duration, err := parseRetryAfterHeader(nil, resp)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if duration != 0 {
			t.Errorf("expected 0 duration for invalid header, got %v", duration)
		}
	})
}

// makeRestyRequest is a helper that makes a resty request and returns the response.
func makeRestyRequest(t *testing.T, url string) *resty.Response {
	t.Helper()

	client := resty.New()
	resp, err := client.R().Get(url)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	return resp
}

package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/slackmgr/types"
)

// sequenceServer responds with the given handlers in order, repeating the last one, and counts the requests.
func sequenceServer(t *testing.T, handlers ...http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var count atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}

		i := int(count.Add(1)) - 1
		if i >= len(handlers) {
			i = len(handlers) - 1
		}

		handlers[i](w, r)
	}))
	t.Cleanup(server.Close)

	return server, &count
}

func rateLimited(retryAfter string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"Rate limit exceeded"}`))
	}
}

func accepted(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusAccepted)
}

func connectedClient(t *testing.T, url string, opts ...Option) *Client {
	t.Helper()

	c := New(url, append([]Option{WithRetryBackoff(100*time.Millisecond, 200*time.Millisecond)}, opts...)...)
	if err := c.Connect(t.Context()); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	t.Cleanup(c.Close)

	return c
}

func TestRetry_RetryAfterNotCappedByRetryMaxWaitTime(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, rateLimited("1"), accepted)
	c := connectedClient(t, server.URL)

	started := time.Now()
	if err := c.Send(t.Context(), types.NewErrorAlert()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RetryMaxWaitTime is 200ms, but the client must wait the full Retry-After second.
	if elapsed := time.Since(started); elapsed < time.Second {
		t.Errorf("expected wait of at least 1s, got %v", elapsed)
	} else if elapsed > 1500*time.Millisecond {
		// Retry-After 1s plus at most 20% jitter, with some slack for scheduling.
		t.Errorf("expected wait of at most 1.2s plus slack, got %v", elapsed)
	}

	if count.Load() != 2 {
		t.Errorf("expected 2 requests, got %d", count.Load())
	}
}

func TestRetry_ShortRetryAfterWaitsInitialBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		retryAfter string
	}{
		{"zero seconds", "0"},
		{"past date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, count := sequenceServer(t, rateLimited(tt.retryAfter), rateLimited(tt.retryAfter), accepted)
			c := connectedClient(t, server.URL, WithRetryBackoff(300*time.Millisecond, 2*time.Second))

			started := time.Now()
			if err := c.Send(t.Context(), types.NewErrorAlert()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Two retries, each waiting the initial backoff (not the exponential backoff), plus at most 20% jitter.
			if elapsed := time.Since(started); elapsed < 600*time.Millisecond {
				t.Errorf("expected wait of at least 600ms, got %v", elapsed)
			} else if elapsed > 1100*time.Millisecond {
				t.Errorf("expected wait of at most 720ms plus slack, got %v", elapsed)
			}

			if count.Load() != 3 {
				t.Errorf("expected 3 requests, got %d", count.Load())
			}
		})
	}
}

func TestRetry_RetryAfterExceedsMaxWait(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, rateLimited("120"))
	c := connectedClient(t, server.URL)

	started := time.Now()
	err := c.Send(t.Context(), types.NewErrorAlert())

	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Errorf("expected immediate give-up, got %v", elapsed)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v", err)
	}

	if !errors.Is(err, ErrRateLimited) {
		t.Error("expected error to match ErrRateLimited")
	}

	if apiErr.RetryAfter != 120*time.Second {
		t.Errorf("expected RetryAfter=120s, got %v", apiErr.RetryAfter)
	}

	if apiErr.Attempts != 1 || count.Load() != 1 {
		t.Errorf("expected 1 attempt, got %d (server saw %d)", apiErr.Attempts, count.Load())
	}
}

func TestRetry_RetryAfterExceedsContextDeadline(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, rateLimited("3"))
	c := connectedClient(t, server.URL)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	err := c.Send(ctx, types.NewErrorAlert())

	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}

	if ctx.Err() != nil {
		t.Error("expected client to give up before the context deadline")
	}

	if count.Load() != 1 {
		t.Errorf("expected 1 request, got %d", count.Load())
	}
}

func TestRetry_BackoffWithoutRetryAfter(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, rateLimited(""))
	c := connectedClient(t, server.URL, WithRetryCount(2))

	err := c.Send(t.Context(), types.NewErrorAlert())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v", err)
	}

	if apiErr.Attempts != 3 || count.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d (server saw %d)", apiErr.Attempts, count.Load())
	}

	if apiErr.RetryAfter != 0 {
		t.Errorf("expected RetryAfter=0, got %v", apiErr.RetryAfter)
	}
}

func TestRetry_NotRetryable(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	c := connectedClient(t, server.URL)

	err := c.Send(t.Context(), types.NewErrorAlert())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v", err)
	}

	if errors.Is(err, ErrRateLimited) {
		t.Error("expected 400 not to match ErrRateLimited")
	}

	if apiErr.StatusCode != http.StatusBadRequest || count.Load() != 1 {
		t.Errorf("expected single 400, got %d after %d requests", apiErr.StatusCode, count.Load())
	}
}

func TestRetry_ContextCancelledDuringWait(t *testing.T) {
	t.Parallel()

	server, _ := sequenceServer(t, rateLimited("2"))
	c := connectedClient(t, server.URL)

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)

	started := time.Now()
	err := c.Send(ctx, types.NewErrorAlert())

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("expected cancellation to stop the wait, took %v", elapsed)
	}

	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected last rate limit error, got %v", err)
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRetry_ContextCancelledDuringBackoff(t *testing.T) {
	t.Parallel()

	server, _ := sequenceServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c := connectedClient(t, server.URL, WithRetryBackoff(2*time.Second, 2*time.Second))

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)

	meta, err := c.SendWithResponse(ctx, types.NewErrorAlert())

	apiErr, ok := errors.AsType[*APIError](err)
	if !ok || apiErr.StatusCode != http.StatusServiceUnavailable || apiErr.Attempts != 1 {
		t.Errorf("expected the 503 of the first attempt, got %v", err)
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if meta == nil || meta.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected the metadata of the first attempt, got %+v", meta)
	}
}

func TestRetry_ContextCancelledDuringBackoffAfterTransportError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	c := connectedClient(t, server.URL, WithRetryBackoff(2*time.Second, 2*time.Second))
	server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)

	meta, err := c.SendWithResponse(ctx, types.NewErrorAlert())

	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "POST alerts failed") {
		t.Errorf("expected the transport error and context.Canceled, got %v", err)
	}

	if meta != nil {
		t.Errorf("expected nil metadata without a response, got %+v", meta)
	}
}

func TestRetry_RedirectsNotRetried(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		maxRedirects int
		requests     int32
	}{
		{"redirects disabled", 0, 1},
		{"redirect limit exceeded", 2, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, count := sequenceServer(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/alerts", http.StatusTemporaryRedirect)
			})
			c := connectedClient(t, server.URL, WithMaxRedirects(tt.maxRedirects))

			meta, err := c.SendWithResponse(t.Context(), types.NewErrorAlert())

			apiErr, ok := errors.AsType[*APIError](err)
			if !ok || apiErr.StatusCode != http.StatusTemporaryRedirect || apiErr.Attempts != 1 {
				t.Errorf("expected a single 307 APIError, got %v", err)
			}

			if meta == nil || meta.StatusCode != http.StatusTemporaryRedirect {
				t.Errorf("expected the metadata of the redirect, got %+v", meta)
			}

			if count.Load() != tt.requests {
				t.Errorf("expected %d requests, got %d", tt.requests, count.Load())
			}
		})
	}
}

func TestRetry_RedirectFollowed(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/alerts", http.StatusTemporaryRedirect)
	}, accepted)
	c := connectedClient(t, server.URL, WithMaxRedirects(1))

	if err := c.Send(t.Context(), types.NewErrorAlert()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count.Load() != 2 {
		t.Errorf("expected 2 requests, got %d", count.Load())
	}
}

func TestRetry_PingAndConnectLimitedBySendTimeout(t *testing.T) {
	t.Parallel()

	var failing atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := []Option{WithSendTimeout(time.Second), WithRetryCount(10), WithRetryBackoff(2*time.Second, 2*time.Second)}

	c := New(server.URL, opts...)
	if err := c.Connect(t.Context()); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	failing.Store(true)

	for name, call := range map[string]func() error{
		"Connect": func() error { return New(server.URL, opts...).Connect(context.Background()) },
		"Ping":    func() error { return c.Ping(context.Background()) },
	} {
		started := time.Now()
		err := call()

		// The 2s backoff would pass the 1s send timeout, so the client stops after the first attempt.
		if elapsed := time.Since(started); elapsed >= time.Second {
			t.Errorf("%s: expected to stop before the send timeout, took %v", name, elapsed)
		}

		if apiErr, ok := errors.AsType[*APIError](err); !ok || apiErr.StatusCode != http.StatusServiceUnavailable || apiErr.Attempts != 1 {
			t.Errorf("%s: expected a single 503, got %v", name, err)
		}
	}
}

func TestRetry_PingRetries(t *testing.T) {
	t.Parallel()

	var count atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if count.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL, WithRetryBackoff(100*time.Millisecond, 200*time.Millisecond))
	if err := c.Connect(t.Context()); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if count.Load() != 2 {
		t.Errorf("expected 2 ping requests, got %d", count.Load())
	}
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		header   string
		expected time.Duration
		ok       bool
	}{
		{"absent", "", 0, false},
		{"seconds", "120", 120 * time.Second, true},
		{"zero seconds", "0", 0, true},
		{"whitespace", " 5 ", 5 * time.Second, true},
		{"negative seconds", "-1", 0, false},
		{"future date", now.Add(time.Minute).Format(http.TimeFormat), time.Minute, true},
		{"past date", now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
		{"invalid", "soon", 0, false},
		{"seconds above the cap", "86401", maxRetryAfter, true},
		{"seconds overflowing time.Duration", "99999999999", maxRetryAfter, true},
		{"seconds overflowing int", "999999999999999999999", 0, false},
		{"date above the cap", now.Add(48 * time.Hour).Format(http.TimeFormat), maxRetryAfter, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			response := &resty.Response{RawResponse: &http.Response{Header: http.Header{}}}
			if tt.header != "" {
				response.RawResponse.Header.Set("Retry-After", tt.header)
			}

			got, ok := retryAfter(response, now)
			if got != tt.expected || ok != tt.ok {
				t.Errorf("expected (%v, %v), got (%v, %v)", tt.expected, tt.ok, got, ok)
			}
		})
	}

	t.Run("nil response", func(t *testing.T) {
		t.Parallel()

		if _, ok := retryAfter(nil, now); ok {
			t.Error("expected false for nil response")
		}

		if _, ok := retryAfter(&resty.Response{}, now); ok {
			t.Error("expected false for response without raw response")
		}
	})
}

func TestBackoff(t *testing.T) {
	t.Parallel()

	minWait := 100 * time.Millisecond
	maxWait := time.Second

	for attempt := 1; attempt <= 10; attempt++ {
		capped := min(maxWait, minWait<<(attempt-1))
		got := backoff(minWait, maxWait, attempt)

		if got < capped/2 || got > capped {
			t.Errorf("attempt %d: expected wait in [%v, %v], got %v", attempt, capped/2, capped, got)
		}
	}

	if got := backoff(0, 0, 1); got != 0 {
		t.Errorf("expected 0 for zero waits, got %v", got)
	}
}

func TestJitter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		wait time.Duration
	}{
		{"zero", 0},
		{"one nanosecond", time.Nanosecond},
		{"one second", time.Second},
		{"one minute", time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			limit := time.Duration(float64(tt.wait) * retryAfterJitter)

			for range 100 {
				if got := jitter(tt.wait); got < 0 || got > limit {
					t.Fatalf("expected jitter in [0, %v], got %v", limit, got)
				}
			}
		})
	}

	// The jitter must actually spread the waits.
	seen := make(map[time.Duration]struct{})
	for range 20 {
		seen[jitter(time.Second)] = struct{}{}
	}

	if len(seen) < 2 {
		t.Error("expected different jitter values")
	}
}

func TestSleep(t *testing.T) {
	t.Parallel()

	if !sleep(t.Context(), 0) {
		t.Error("expected true for zero wait with live context")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if sleep(ctx, 0) {
		t.Error("expected false for zero wait with cancelled context")
	}

	if sleep(ctx, time.Hour) {
		t.Error("expected false when context is cancelled")
	}
}

func TestAPIError(t *testing.T) {
	t.Parallel()

	err := &APIError{Method: http.MethodPost, URL: "http://x/alerts", StatusCode: 429, Message: "Rate limit exceeded"}

	if !strings.Contains(err.Error(), "POST http://x/alerts failed with status code 429: Rate limit exceeded") {
		t.Errorf("unexpected message: %s", err.Error())
	}

	if !errors.Is(err, ErrRateLimited) {
		t.Error("expected 429 to match ErrRateLimited")
	}

	err.StatusCode = 503
	if errors.Is(err, ErrRateLimited) {
		t.Error("expected 503 not to match ErrRateLimited")
	}
}

func TestRetry_CustomCondition(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}, accepted)

	var gotStatus atomic.Int32

	c := connectedClient(t, server.URL, WithRetryCondition(func(statusCode int, err error) bool {
		gotStatus.Store(int32(statusCode)) // #nosec G115 -- HTTP status codes fit in int32
		return statusCode == http.StatusConflict || DefaultRetryCondition(statusCode, err)
	}))

	if err := c.Send(t.Context(), types.NewErrorAlert()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count.Load() != 2 || gotStatus.Load() != http.StatusConflict {
		t.Errorf("expected a retry of the 409, got %d requests (last status %d)", count.Load(), gotStatus.Load())
	}
}

func TestRetry_SendTimeoutWithoutContextDeadline(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, rateLimited("3"), accepted)
	c := connectedClient(t, server.URL, WithSendTimeout(time.Second))

	started := time.Now()
	err := c.Send(context.Background(), types.NewErrorAlert())

	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}

	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Errorf("expected immediate give-up, got %v", elapsed)
	}

	if count.Load() != 1 {
		t.Errorf("expected 1 request, got %d", count.Load())
	}
}

func TestRetry_ContextDeadlineOverridesSendTimeout(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, rateLimited("1"), accepted)
	c := connectedClient(t, server.URL, WithSendTimeout(time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	if err := c.Send(ctx, types.NewErrorAlert()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count.Load() != 2 {
		t.Errorf("expected 2 requests, got %d", count.Load())
	}
}

func TestRetry_SendTimeoutDisabled(t *testing.T) {
	t.Parallel()

	server, count := sequenceServer(t, rateLimited("1"), accepted)
	c := connectedClient(t, server.URL, WithSendTimeout(0))

	if err := c.Send(context.Background(), types.NewErrorAlert()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count.Load() != 2 {
		t.Errorf("expected 2 requests, got %d", count.Load())
	}
}

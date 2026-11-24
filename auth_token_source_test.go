package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slackmgr/types"
)

func TestWithAuthTokenSource(t *testing.T) {
	t.Parallel()

	opts := newClientOptions()
	WithAuthTokenSource(nil)(opts)

	if opts.authTokenSource != nil {
		t.Error("expected nil token source to be ignored")
	}

	WithAuthTokenSource(func(context.Context) (string, error) { return "t", nil })(opts)

	if opts.authTokenSource == nil {
		t.Error("expected token source to be set")
	}
}

func TestAuthTokenSource_SetsTokenOnEveryAttempt(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		headers []string
		posts   atomic.Int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = append(headers, r.Method+" "+r.Header.Get("Authorization"))
		mu.Unlock()

		// Fail the first POST, so that it is retried.
		if r.Method == http.MethodPost && posts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	type ctxKey struct{}

	var calls atomic.Int32

	source := func(ctx context.Context) (string, error) {
		if ctx.Value(ctxKey{}) != "send" && ctx.Value(ctxKey{}) != "connect" {
			return "", errors.New("unexpected context")
		}

		return "token-" + strconv.Itoa(int(calls.Add(1))), nil
	}

	client := New(server.URL, WithAuthTokenSource(source), WithRetryBackoff(100*time.Millisecond, 100*time.Millisecond))

	if err := client.Connect(context.WithValue(context.Background(), ctxKey{}, "connect")); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if err := client.Send(context.WithValue(context.Background(), ctxKey{}, "send"), types.NewInfoAlert()); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	want := []string{"GET Bearer token-1", "POST Bearer token-2", "POST Bearer token-3"}
	if strings.Join(headers, "|") != strings.Join(want, "|") {
		t.Errorf("expected headers %v, got %v", want, headers)
	}
}

func TestAuthTokenSource_UsesAuthScheme(t *testing.T) {
	t.Parallel()

	var authHeader atomic.Value

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, WithAuthScheme("Token"), WithAuthTokenSource(func(context.Context) (string, error) { return "abc", nil }))

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if got := authHeader.Load(); got != "Token abc" {
		t.Errorf("expected Authorization=Token abc, got %v", got)
	}
}

func TestAuthTokenSource_ErrorFailsWithoutRetry(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tokenErr := errors.New("token endpoint unavailable")

	var calls atomic.Int32

	source := func(context.Context) (string, error) {
		// Succeed for Connect, fail for Send.
		if calls.Add(1) == 1 {
			return "ok", nil
		}

		return "", tokenErr
	}

	client := New(server.URL, WithAuthTokenSource(source), WithRetryCount(3))

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	err := client.Send(context.Background(), types.NewInfoAlert())
	if !errors.Is(err, tokenErr) {
		t.Fatalf("expected token error, got %v", err)
	}

	if !strings.Contains(err.Error(), "failed to get auth token") {
		t.Errorf("unexpected error message: %v", err)
	}

	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 token source calls (no retries), got %d", got)
	}

	if got := requests.Load(); got != 1 {
		t.Errorf("expected only the connect request to reach the server, got %d", got)
	}
}

package client

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/slackmgr/types"
)

// fakeClock is a manually advanced clock for pacer tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// rateLimitResponse builds a response with the given status and headers (name, value pairs).
func rateLimitResponse(status int, headers ...string) *resty.Response {
	h := http.Header{}
	for i := 0; i+1 < len(headers); i += 2 {
		h.Set(headers[i], headers[i+1])
	}

	return &resty.Response{RawResponse: &http.Response{StatusCode: status, Header: h}}
}

// learnedPacer returns a pacer that knows a burst of 10 and a rate of 1 token per second, with target "a" at
// the given remaining tokens.
func learnedPacer(t *testing.T, clock *fakeClock, remaining int) *pacer {
	t.Helper()

	p := newPacer(clock.Now)
	counts := map[string]int{"a": 1}
	_, _, res := p.reserve(counts)
	p.sent(res)
	p.update(counts, res, rateLimitResponse(http.StatusAccepted,
		"Ratelimit-Policy", `"channel";q=10;w=10`,
		"Ratelimit-Limit", "10",
		"Ratelimit-Remaining", strconv.Itoa(remaining),
		"Ratelimit-Reset", "5",
	))

	return p
}

func TestPacer_UnknownLimitsNeverWait(t *testing.T) {
	t.Parallel()

	p := newPacer(newFakeClock().Now)

	for range 100 {
		wait, _, _ := p.reserve(map[string]int{"a": 5})
		if wait != 0 {
			t.Fatalf("expected no wait before limits are known, got %v", wait)
		}
	}
}

func TestPacer_WaitsForDeficit(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	p := learnedPacer(t, clock, 2)

	wait, target, _ := p.reserve(map[string]int{"a": 5})
	if wait != 3*time.Second || target != "a" {
		t.Errorf("expected 3s wait for a, got %v for %q", wait, target)
	}

	// The tokens are reserved, so the next request queues behind it.
	wait, _, _ = p.reserve(map[string]int{"a": 1})
	if wait != 4*time.Second {
		t.Errorf("expected 4s wait behind the reservation, got %v", wait)
	}
}

func TestPacer_Refills(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	p := learnedPacer(t, clock, 0)

	clock.Advance(3 * time.Second)

	if wait, _, _ := p.reserve(map[string]int{"a": 3}); wait != 0 {
		t.Errorf("expected no wait after refill, got %v", wait)
	}

	clock.Advance(time.Hour)

	// Refill is capped at the burst.
	if wait, _, _ := p.reserve(map[string]int{"a": 10}); wait != 0 {
		t.Errorf("expected no wait for a full burst, got %v", wait)
	}

	if wait, _, _ := p.reserve(map[string]int{"a": 1}); wait != time.Second {
		t.Errorf("expected 1s wait after using the full burst, got %v", wait)
	}
}

func TestPacer_UnknownTargetIsFull(t *testing.T) {
	t.Parallel()

	p := learnedPacer(t, newFakeClock(), 0)

	if wait, _, _ := p.reserve(map[string]int{"b": 10}); wait != 0 {
		t.Errorf("expected no wait for an unknown target, got %v", wait)
	}
}

func TestPacer_LongestTargetWins(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	p := learnedPacer(t, clock, 0)

	wait, target, _ := p.reserve(map[string]int{"a": 2, "b": 1})
	if wait != 2*time.Second || target != "a" {
		t.Errorf("expected 2s for a, got %v for %q", wait, target)
	}
}

func TestPacer_CountAboveBurstIsLeftToTheAPI(t *testing.T) {
	t.Parallel()

	p := learnedPacer(t, newFakeClock(), 0)

	wait, _, res := p.reserve(map[string]int{"a": 11})
	if wait != 0 {
		t.Errorf("expected no wait for a count above the burst, got %v", wait)
	}

	if len(res.counts) != 0 {
		t.Errorf("expected nothing reserved, got %v", res.counts)
	}
}

func TestPacer_CancelRefunds(t *testing.T) {
	t.Parallel()

	p := learnedPacer(t, newFakeClock(), 5)

	_, _, res := p.reserve(map[string]int{"a": 5})
	p.cancel(res)
	p.cancel(res) // idempotent

	if wait, _, _ := p.reserve(map[string]int{"a": 5}); wait != 0 {
		t.Errorf("expected refunded tokens, got wait %v", wait)
	}
}

func TestPacer_ResyncKeepsWaitingReservations(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	p := learnedPacer(t, clock, 2)

	// A is sent; B reserves and waits.
	countsA := map[string]int{"a": 2}
	_, _, resA := p.reserve(countsA)
	p.sent(resA)

	_, _, resB := p.reserve(map[string]int{"a": 1})
	if !resB.waiting {
		t.Fatal("expected B to be waiting")
	}

	// A's response says 0 remaining. B's waiting token must still count against the bucket.
	p.update(countsA, resA, rateLimitResponse(http.StatusAccepted, "Ratelimit-Limit", "10", "Ratelimit-Remaining", "0"))

	if wait, _, _ := p.reserve(map[string]int{"a": 1}); wait != 2*time.Second {
		t.Errorf("expected 2s wait behind B, got %v", wait)
	}
}

func TestPacer_TooManyRequestsBlocksUntilRetryAfter(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	p := learnedPacer(t, clock, 10)

	counts := map[string]int{"a": 2, "b": 1}
	_, _, res := p.reserve(counts)
	p.sent(res)
	p.update(counts, res, rateLimitResponse(http.StatusTooManyRequests, "Retry-After", "4", "Ratelimit-Limit", "10", "Ratelimit-Remaining", "0"))

	wait, _, _ := p.reserve(map[string]int{"a": 2})
	if wait != 4*time.Second {
		t.Errorf("expected 4s wait for a, got %v", wait)
	}

	clock.Advance(4 * time.Second)

	// b needed 1 token 4s ago, and got its own wait of 4s.
	if wait, _, _ := p.reserve(map[string]int{"b": 1}); wait != 0 {
		t.Errorf("expected no wait for b after Retry-After, got %v", wait)
	}
}

func TestPacer_ErrorWithoutHeadersRefunds(t *testing.T) {
	t.Parallel()

	p := learnedPacer(t, newFakeClock(), 3)

	counts := map[string]int{"a": 3}
	_, _, res := p.reserve(counts)
	p.sent(res)
	p.update(counts, res, rateLimitResponse(http.StatusBadRequest))

	if wait, _, _ := p.reserve(counts); wait != 0 {
		t.Errorf("expected refund after 400, got wait %v", wait)
	}
}

func TestPacer_NoResponseRefunds(t *testing.T) {
	t.Parallel()

	for name, response := range map[string]*resty.Response{"nil response": nil, "no raw response": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := learnedPacer(t, newFakeClock(), 3)

			counts := map[string]int{"a": 3}
			_, _, res := p.reserve(counts)
			p.sent(res)
			p.update(counts, res, response)

			// A retry of the same request is not delayed by the tokens of the attempt without a response.
			if wait, _, _ := p.reserve(counts); wait != 0 {
				t.Errorf("expected refund without a response, got wait %v", wait)
			}
		})
	}
}

func TestPacer_InfersRateWithoutPolicy(t *testing.T) {
	t.Parallel()

	p := newPacer(newFakeClock().Now)
	counts := map[string]int{"a": 1}
	_, _, res := p.reserve(counts)
	p.sent(res)
	p.update(counts, res, rateLimitResponse(http.StatusAccepted, "Ratelimit-Limit", "10", "Ratelimit-Remaining", "6", "Ratelimit-Reset", "8"))

	if p.rate != 0.5 || p.burst != 10 {
		t.Errorf("expected rate 0.5 and burst 10, got %v and %v", p.rate, p.burst)
	}

	// A policy overrides the estimate, and later estimates do not override the policy.
	p.update(counts, nil, rateLimitResponse(http.StatusAccepted, "Ratelimit-Policy", `"channel";q=10;w=5`, "Ratelimit-Limit", "10"))
	p.update(counts, nil, rateLimitResponse(http.StatusAccepted, "Ratelimit-Limit", "10", "Ratelimit-Remaining", "6", "Ratelimit-Reset", "8"))

	if p.rate != 2 {
		t.Errorf("expected rate 2 from the policy, got %v", p.rate)
	}
}

func TestPacer_Sweep(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	p := learnedPacer(t, clock, 0)

	_, _, res := p.reserve(map[string]int{"b": 1}) // b keeps a waiting reservation

	clock.Advance(2 * pacerSweepInterval)
	p.reserve(map[string]int{})

	if _, ok := p.buckets["a"]; ok {
		t.Error("expected full bucket a to be removed")
	}

	if _, ok := p.buckets["b"]; !ok {
		t.Error("expected bucket b with a waiting reservation to be kept")
	}

	p.sent(res)
}

func TestPacingTargets(t *testing.T) {
	t.Parallel()

	alerts := []*types.Alert{
		{SlackChannelID: "C1"},
		{SlackChannelID: " C1 "},
		{RouteKey: "Team-A"},
		{RouteKey: "team-a"},
		{SlackChannelID: "C2", RouteKey: "team-a"},
	}

	got := pacingTargets(alerts)

	if got["channel:C1"] != 2 || got["route:team-a"] != 2 || got["channel:C2"] != 1 || len(got) != 3 {
		t.Errorf("unexpected targets: %v", got)
	}
}

func TestParseRateLimitPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value  string
		quota  int
		window int
		ok     bool
	}{
		{`"channel";q=30;w=30`, 30, 30, true},
		{`"default";q=100;w=60`, 100, 60, true},
		{`"burst";q=100;w=60,"daily";q=1000;w=86400`, 100, 60, true},
		{`"peruser";q=65535;qu="content-bytes";w=10;pk=:sdfjLJUOUH==:`, 65535, 10, true},
		{`"a";q=10, "b";q=5;w=1`, 5, 1, true},
		{`"default";q=100`, 0, 0, false},
		{`"default";q=0;w=60`, 0, 0, false},
		{`"default";q=99999999999999999999;w=60`, 0, 0, false},
		{``, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			quota, window, ok := parseRateLimitPolicy(tt.value)
			if quota != tt.quota || window != tt.window || ok != tt.ok {
				t.Errorf("expected (%d, %d, %v), got (%d, %d, %v)", tt.quota, tt.window, tt.ok, quota, window, ok)
			}
		})
	}
}

// bucketServer simulates the API's rate limiter for one channel: a token bucket with the given burst and
// rate, returning the RateLimit-* headers and 429 with Retry-After like the API does.
type bucketServer struct {
	mu       sync.Mutex
	burst    float64
	rate     float64
	tokens   float64
	at       time.Time
	accepted atomic.Int32
	rejected atomic.Int32
}

func (b *bucketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ping" {
		w.WriteHeader(http.StatusOK)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.tokens = min(b.burst, b.tokens+now.Sub(b.at).Seconds()*b.rate)
	b.at = now

	window := max(1, math.Round(b.burst/b.rate)) // same encoding as the API
	w.Header().Set("Ratelimit-Policy", `"channel";q=`+strconv.Itoa(int(math.Round(b.rate*window)))+`;w=`+strconv.Itoa(int(window)))
	w.Header().Set("Ratelimit-Limit", strconv.Itoa(int(b.burst)))

	if b.tokens < 1 {
		retry := int((1 - b.tokens) / b.rate)
		if float64(retry) < (1-b.tokens)/b.rate {
			retry++
		}

		w.Header().Set("Retry-After", strconv.Itoa(retry))
		w.Header().Set("Ratelimit-Remaining", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		b.rejected.Add(1)

		return
	}

	b.tokens--
	w.Header().Set("Ratelimit-Remaining", strconv.Itoa(int(b.tokens)))
	w.WriteHeader(http.StatusAccepted)
	b.accepted.Add(1)
}

func TestClient_PacingAvoidsTooManyRequests(t *testing.T) {
	t.Parallel()

	b := &bucketServer{burst: 2, rate: 10, tokens: 2, at: time.Now()}
	server := httptest.NewServer(b)
	defer server.Close()

	c := connectedClient(t, server.URL, WithRetryCount(0))

	// The first request teaches the client the limits, then it paces the rest: 6 alerts at 10/s with a burst
	// of 2 take about 0.4s, without any 429.
	for range 6 {
		if err := c.Send(t.Context(), &types.Alert{SlackChannelID: "C1"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if b.accepted.Load() != 6 || b.rejected.Load() != 0 {
		t.Errorf("expected 6 accepted and 0 rejected, got %d and %d", b.accepted.Load(), b.rejected.Load())
	}
}

func TestClient_PacingConcurrentSenders(t *testing.T) {
	t.Parallel()

	b := &bucketServer{burst: 2, rate: 20, tokens: 2, at: time.Now()}
	server := httptest.NewServer(b)
	defer server.Close()

	c := connectedClient(t, server.URL)

	// Learn the limits first.
	if err := c.Send(t.Context(), &types.Alert{SlackChannelID: "C1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			if err := c.Send(t.Context(), &types.Alert{SlackChannelID: "C1"}); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	wg.Wait()

	if b.accepted.Load() != 9 {
		t.Errorf("expected 9 accepted, got %d", b.accepted.Load())
	}

	// Reservations queue the senders; allow a rare 429 from timing jitter, which the retry handles.
	if b.rejected.Load() > 1 {
		t.Errorf("expected at most 1 rejection, got %d", b.rejected.Load())
	}
}

func TestClient_PacingNotSent(t *testing.T) {
	t.Parallel()

	b := &bucketServer{burst: 2, rate: 0.01, tokens: 2, at: time.Now()}
	server := httptest.NewServer(b)
	defer server.Close()

	c := connectedClient(t, server.URL)

	for range 2 {
		if err := c.Send(t.Context(), &types.Alert{SlackChannelID: "C1"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// The bucket is empty and refills in 100s, beyond the default 1m max wait: fail without sending.
	meta, err := c.SendWithResponse(t.Context(), &types.Alert{SlackChannelID: "C1"})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v", err)
	}

	if !apiErr.NotSent || apiErr.Attempts != 0 || !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected unsent rate limit error with 0 attempts, got %+v", apiErr)
	}

	if apiErr.RetryAfter < 99*time.Second || apiErr.RetryAfter > 100*time.Second {
		t.Errorf("expected RetryAfter ~100s, got %v", apiErr.RetryAfter)
	}

	if meta != nil {
		t.Errorf("expected nil metadata, got %+v", meta)
	}

	if b.accepted.Load()+b.rejected.Load() != 2 {
		t.Errorf("expected only 2 requests to reach the server, got %d", b.accepted.Load()+b.rejected.Load())
	}

	// Other targets are not paced. The fake server has a single bucket, so C2 gets a real 429.
	err = c.Send(t.Context(), &types.Alert{SlackChannelID: "C2"})
	if !errors.Is(err, ErrRateLimited) || isNotSent(err) {
		t.Errorf("expected C2 to be sent and rejected by the server, got %v", err)
	}
}

func isNotSent(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.NotSent
}

func TestClient_PacingContextDeadline(t *testing.T) {
	t.Parallel()

	b := &bucketServer{burst: 1, rate: 0.5, tokens: 1, at: time.Now()}
	server := httptest.NewServer(b)
	defer server.Close()

	c := connectedClient(t, server.URL)

	if err := c.Send(t.Context(), &types.Alert{SlackChannelID: "C1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	err := c.Send(ctx, &types.Alert{SlackChannelID: "C1"})
	if !isNotSent(err) {
		t.Errorf("expected unsent error when the wait exceeds the deadline, got %v", err)
	}
}

func TestClient_PacingCancelledWhileWaiting(t *testing.T) {
	t.Parallel()

	b := &bucketServer{burst: 1, rate: 0.5, tokens: 1, at: time.Now()}
	server := httptest.NewServer(b)
	defer server.Close()

	c := connectedClient(t, server.URL)

	if err := c.Send(t.Context(), &types.Alert{SlackChannelID: "C1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)

	err := c.Send(ctx, &types.Alert{SlackChannelID: "C1"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if b.accepted.Load()+b.rejected.Load() != 1 {
		t.Errorf("expected the cancelled request not to be sent, got %d requests", b.accepted.Load()+b.rejected.Load())
	}
}

func TestClient_PacingDisabled(t *testing.T) {
	t.Parallel()

	b := &bucketServer{burst: 1, rate: 0.01, tokens: 1, at: time.Now()}
	server := httptest.NewServer(b)
	defer server.Close()

	c := connectedClient(t, server.URL, WithRateLimitPacing(false), WithRetryCount(0))

	if c.pacer != nil {
		t.Fatal("expected no pacer")
	}

	_ = c.Send(t.Context(), &types.Alert{SlackChannelID: "C1"})
	err := c.Send(t.Context(), &types.Alert{SlackChannelID: "C1"})

	if !errors.Is(err, ErrRateLimited) || isNotSent(err) {
		t.Errorf("expected a 429 from the server, got %v", err)
	}
}

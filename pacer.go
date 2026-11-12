package client

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/slackmgr/types"
)

// pacerSweepInterval is how often the pacer removes buckets that have refilled completely.
const pacerSweepInterval = time.Minute

var (
	policyQuotaRegex  = regexp.MustCompile(`;\s*q=(\d+)`)
	policyWindowRegex = regexp.MustCompile(`;\s*w=(\d+)`)
)

// pacer mirrors the API's per-channel token bucket rate limiter, so that the client can hold back requests the
// API would certainly reject with 429, instead of sending them and retrying.
//
// The bucket size (burst) and refill rate are learned from the RateLimit-Limit and RateLimit-Policy response
// headers, and the buckets of a request's targets are resynced from RateLimit-Remaining (or Retry-After on 429)
// after every response, so the API stays authoritative. Until the limits are known, and for targets without a
// bucket, the pacer assumes full buckets, i.e. it never delays requests it has no information about.
//
// Whenever it is unsure, the pacer errs on the optimistic side (more tokens): other senders to the same channel
// only drain tokens, attempts without a response give their tokens back, and so do alerts the API ignores. So it
// delays requests that would be rejected, and with rare exceptions not requests that would succeed: the tokens
// of alerts the API ignored in a request with several targets are only returned up to RateLimit-Remaining of the
// most constrained channel, and a request that started waiting behind a reservation that is cancelled later keeps
// waiting as long as computed.
type pacer struct {
	mu        sync.Mutex
	now       func() time.Time
	burst     float64 // bucket size, 0 if unknown
	rate      float64 // tokens per second, 0 if unknown
	fromQuota bool    // rate comes from RateLimit-Policy (rather than inferred)
	buckets   map[string]*bucket
	waiting   map[string]int // tokens reserved by requests not sent yet, per target
	lastSweep time.Time
}

// bucket is the token count of one target at a point in time. Tokens may be negative when reserved ahead.
type bucket struct {
	tokens float64
	at     time.Time
}

// reservation holds the tokens taken by [pacer.reserve], so they can be returned if the request is not sent.
type reservation struct {
	counts  map[string]int
	active  bool // the tokens are held
	waiting bool // the request has not been sent yet
}

func newPacer(now func() time.Time) *pacer {
	return &pacer{
		now:     now,
		buckets: make(map[string]*bucket),
		waiting: make(map[string]int),
	}
}

// pacingTargets counts the alerts per target, keyed by Slack channel (ID or name) or route key, as sent. The
// API's route key and channel name mappings are unknown to the client, so targets that map to the same channel
// get separate buckets. That only makes the estimate more optimistic, never more restrictive.
func pacingTargets(alerts []*types.Alert) map[string]int {
	counts := make(map[string]int)

	for _, alert := range alerts {
		key := "channel:" + strings.TrimSpace(alert.SlackChannelID)
		if strings.TrimSpace(alert.SlackChannelID) == "" {
			key = "route:" + strings.ToLower(strings.TrimSpace(alert.RouteKey))
		}

		counts[key]++
	}

	return counts
}

// reserve takes the tokens for counts and returns the wait until all targets have enough tokens, and the target
// that needs the longest wait. The tokens are taken immediately, so concurrent callers queue up behind each other
// rather than waking up at the same time. Call [pacer.cancel] if the request is not sent.
func (p *pacer) reserve(counts map[string]int) (time.Duration, string, *reservation) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	p.sweep(now)

	if p.burst <= 0 || p.rate <= 0 {
		return 0, "", &reservation{}
	}

	var (
		wait   time.Duration
		target string
	)

	for key, count := range counts {
		// The API rejects requests exceeding the burst for a channel with 400; let it say so.
		if float64(count) > p.burst {
			continue
		}

		b := p.bucket(key, now)

		if deficit := float64(count) - b.tokens; deficit > 0 {
			if w := time.Duration(deficit / p.rate * float64(time.Second)); w > wait {
				wait, target = w, key
			}
		}
	}

	reserved := make(map[string]int, len(counts))

	for key, count := range counts {
		if float64(count) > p.burst {
			continue
		}

		p.bucket(key, now).tokens -= float64(count)
		p.waiting[key] += count
		reserved[key] = count
	}

	return wait, target, &reservation{counts: reserved, active: true, waiting: true}
}

// sent marks the request of a reservation as sent. Call it right before sending.
func (p *pacer) sent(r *reservation) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.release(r)
}

// release removes the reservation from the waiting tokens. Must be called with p.mu held.
func (p *pacer) release(r *reservation) {
	if r == nil || !r.waiting {
		return
	}

	r.waiting = false

	for key, count := range r.counts {
		if p.waiting[key] -= count; p.waiting[key] <= 0 {
			delete(p.waiting, key)
		}
	}
}

// cancel returns the tokens of a reservation whose request was not sent.
func (p *pacer) cancel(r *reservation) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.release(r)
	p.refund(r)
}

// update learns the limits from the response headers and resyncs the buckets of the targets in counts. The
// tokens of the reservation are no longer active afterwards.
//
// Without a response (a transport error), the tokens of the reservation are returned. The request may have been
// processed (e.g. on a timeout), but usually it was not (e.g. connection refused while the API restarts), and
// keeping the tokens would delay every retry by the time to refill them. Returning them errs on the optimistic
// side, as the pacer always does: the API corrects the estimate with its next response.
func (p *pacer) update(counts map[string]int, r *reservation, response *resty.Response) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if response == nil || response.RawResponse == nil {
		p.refund(r)
		return
	}

	defer p.deactivate(r)

	now := p.now()
	header := response.Header()

	limit, hasLimit := headerInt(header, "Ratelimit-Limit")
	remaining, hasRemaining := headerInt(header, "Ratelimit-Remaining")
	reset, hasReset := headerInt(header, "Ratelimit-Reset")

	if hasLimit && limit > 0 {
		p.burst = float64(limit)
	}

	if quota, window, ok := parseRateLimitPolicy(header.Get("Ratelimit-Policy")); ok {
		p.rate = float64(quota) / float64(window)
		p.fromQuota = true
	} else if !p.fromQuota && hasLimit && hasRemaining && hasReset && reset > 0 && remaining < limit {
		// Older servers without RateLimit-Policy: estimate the rate from the time to refill the bucket.
		p.rate = float64(limit-remaining) / float64(reset)
	}

	if p.burst <= 0 || p.rate <= 0 {
		return
	}

	switch {
	case response.StatusCode() == http.StatusTooManyRequests:
		// No tokens were taken. Make sure no target can afford its alerts before Retry-After has passed. With
		// several targets the rate limited one is unknown, so this applies to all of them, but only until then.
		p.refund(r)

		wait, ok := retryAfter(response, now)
		if !ok {
			return
		}

		for key, count := range counts {
			b := p.bucket(key, now)
			b.tokens = min(b.tokens, float64(count)-wait.Seconds()*p.rate)
		}
	case !hasRemaining:
		// The API took no tokens: the request was rejected before the rate limit check, all its alerts were
		// ignored (204), or the API's rate limiter failed open.
		p.refund(r)
	case len(counts) == 1:
		// Exact state of the single target, less the tokens reserved by requests that are still waiting.
		// RateLimit-Remaining is rounded down, so this errs on the safe side. It also returns the tokens of
		// alerts the API ignored, as they take no tokens.
		for key := range counts {
			p.buckets[key] = &bucket{tokens: float64(remaining - p.waiting[key]), at: now}
		}
	default:
		// RateLimit-Remaining is that of the most constrained channel of the request, so every target has at
		// least that many tokens. This returns tokens the client took for alerts the API ignored. If all
		// alerts of a target were ignored, the API did not check its channel, and this is optimistic.
		for key := range counts {
			b := p.bucket(key, now)
			b.tokens = max(b.tokens, float64(remaining-p.waiting[key]))
		}
	}
}

// deactivate marks the tokens of a reservation as no longer held, without returning them. Must be called with
// p.mu held.
func (p *pacer) deactivate(r *reservation) {
	if r != nil {
		r.active = false
	}
}

// bucket returns the bucket of key, refilled up to now. A missing bucket is full. Must be called with p.mu held.
func (p *pacer) bucket(key string, now time.Time) *bucket {
	b, ok := p.buckets[key]
	if !ok {
		b = &bucket{tokens: p.burst, at: now}
		p.buckets[key] = b

		return b
	}

	if elapsed := now.Sub(b.at).Seconds(); elapsed > 0 {
		b.tokens = min(p.burst, b.tokens+elapsed*p.rate)
		b.at = now
	}

	return b
}

// refund returns the tokens of an active reservation. Must be called with p.mu held.
func (p *pacer) refund(r *reservation) {
	if r == nil || !r.active {
		return
	}

	r.active = false
	now := p.now()

	for key, count := range r.counts {
		if p.burst > 0 && p.rate > 0 {
			b := p.bucket(key, now)
			b.tokens = min(p.burst, b.tokens+float64(count))
		}
	}
}

// sweep removes the buckets that have refilled completely, as they are identical to missing ones.
// Must be called with p.mu held.
func (p *pacer) sweep(now time.Time) {
	if now.Sub(p.lastSweep) < pacerSweepInterval {
		return
	}

	p.lastSweep = now

	for key := range p.buckets {
		if p.waiting[key] > 0 {
			continue
		}

		if p.burst <= 0 || p.rate <= 0 || p.bucket(key, now).tokens >= p.burst {
			delete(p.buckets, key)
		}
	}
}

// parseRateLimitPolicy returns the quota and window (seconds) of the first policy with both in a RateLimit-Policy
// header (draft-ietf-httpapi-ratelimit-headers-11), e.g. `"channel";q=30;w=30`.
func parseRateLimitPolicy(value string) (int, int, bool) {
	for item := range strings.SplitSeq(value, ",") {
		q := policyQuotaRegex.FindStringSubmatch(item)
		w := policyWindowRegex.FindStringSubmatch(item)

		if q == nil || w == nil {
			continue
		}

		quota, errQ := strconv.Atoi(q[1])
		window, errW := strconv.Atoi(w[1])

		if errQ == nil && errW == nil && quota > 0 && window > 0 {
			return quota, window, true
		}
	}

	return 0, 0, false
}

// headerInt returns the non-negative integer value of a header.
func headerInt(header http.Header, name string) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(header.Get(name)))
	if err != nil || v < 0 {
		return 0, false
	}

	return v, true
}

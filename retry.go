package client

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// execute sends the request, retrying according to the retry policy. It returns the last response and error,
// and the number of attempts made.
//
// The wait between attempts is the Retry-After header value when present (up to maxRateLimitWait), and capped
// exponential backoff with jitter otherwise. Retrying stops early when the API asks for a longer wait than
// maxRateLimitWait, or when the wait would exceed the context deadline, as the retry could not succeed anyway.
//
// When pacing is enabled and targets is non-nil, each attempt first waits until the client's model of the API's
// rate limiter (see [pacer]) has tokens for all targets. If that wait exceeds maxRateLimitWait or the context
// deadline, the request is not sent and an [*APIError] with status 429 and [APIError.NotSent] is returned.
func (c *Client) execute(ctx context.Context, client *resty.Client, method, path string, body []byte, targets map[string]int) (*resty.Response, int, error) {
	var lastResponse *resty.Response

	for attempt := 1; ; attempt++ {
		res, err := c.pace(ctx, method, path, targets, attempt)
		if err != nil {
			return lastResponse, attempt - 1, err
		}

		request := client.R().SetContext(ctx)
		if body != nil {
			request.SetBody(body)
		}

		response, err := request.Execute(method, path)
		lastResponse = response

		if res != nil {
			c.pacer.update(targets, res, response)
		}

		failed := err != nil || response == nil || !response.IsSuccess()

		if !failed || ctx.Err() != nil || attempt > c.options.retryCount || !c.options.retryPolicy(response, err) {
			return response, attempt, err
		}

		wait, fromHeader := c.retryWait(response, attempt)

		if fromHeader && wait > c.options.maxRateLimitWait {
			c.options.requestLogger.Warnf("%s %s: not retrying, Retry-After %v exceeds the max wait %v", method, path, wait, c.options.maxRateLimitWait)
			return response, attempt, err
		}

		if deadline, ok := ctx.Deadline(); ok && time.Now().Add(wait).After(deadline) {
			c.options.requestLogger.Warnf("%s %s: not retrying, wait %v exceeds the context deadline", method, path, wait)
			return response, attempt, err
		}

		c.options.requestLogger.Debugf("%s %s: attempt %d failed, retrying in %v", method, path, attempt, wait)

		if !sleep(ctx, wait) {
			return response, attempt, err
		}
	}
}

// pace waits until the pacer has tokens for the targets, and marks the returned reservation as sent. It returns
// nil without waiting when pacing is disabled or targets is nil.
func (c *Client) pace(ctx context.Context, method, path string, targets map[string]int, attempt int) (*reservation, error) {
	if c.pacer == nil || targets == nil {
		return nil, nil //nolint:nilnil // no reservation and no error when not pacing
	}

	wait, target, res := c.pacer.reserve(targets)

	if wait > 0 {
		exceedsMax := wait > c.options.maxRateLimitWait
		deadline, hasDeadline := ctx.Deadline()

		if exceedsMax || (hasDeadline && time.Now().Add(wait).After(deadline)) {
			c.pacer.cancel(res)
			c.options.requestLogger.Warnf("%s %s: not sent, client-side rate limit for %s requires a wait of %v", method, path, target, wait)

			return nil, &APIError{
				Method:     method,
				URL:        sanitizeURL(strings.TrimSuffix(c.baseURL, "/") + "/" + strings.TrimPrefix(path, "/")),
				StatusCode: http.StatusTooManyRequests,
				Message:    fmt.Sprintf("Rate limit exceeded for %s (predicted by the client, request not sent)", target),
				RetryAfter: wait,
				Attempts:   attempt - 1,
				NotSent:    true,
			}
		}

		c.options.requestLogger.Debugf("%s %s: pacing, waiting %v for the rate limit of %s", method, path, wait, target)

		if !sleep(ctx, wait) {
			c.pacer.cancel(res)
			return nil, ctx.Err()
		}
	}

	c.pacer.sent(res)

	return res, nil
}

// retryWait returns the wait before the next attempt, and whether it comes from the Retry-After header.
func (c *Client) retryWait(response *resty.Response, attempt int) (time.Duration, bool) {
	if wait, ok := retryAfter(response, time.Now()); ok {
		return wait, true
	}

	return backoff(c.options.retryWaitTime, c.options.retryMaxWaitTime, attempt), false
}

// retryAfter parses the Retry-After header of the response, as either delay seconds or an HTTP date relative
// to now. A date in the past gives zero, i.e. retry immediately. The bool is false when the header is absent or
// invalid.
func retryAfter(response *resty.Response, now time.Time) (time.Duration, bool) {
	if response == nil || response.RawResponse == nil {
		return 0, false
	}

	value := strings.TrimSpace(response.Header().Get("Retry-After"))
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}

		return time.Duration(seconds) * time.Second, true
	}

	if t, err := http.ParseTime(value); err == nil {
		return max(t.Sub(now), 0), true
	}

	return 0, false
}

// backoff returns the capped exponential backoff with equal jitter for the given attempt (1-based): a random
// duration between half and all of min(maxWait, minWait * 2^(attempt-1)).
func backoff(minWait, maxWait time.Duration, attempt int) time.Duration {
	capped := math.Min(float64(maxWait), float64(minWait)*math.Exp2(float64(attempt-1)))
	half := time.Duration(capped / 2)

	if half <= 0 {
		return minWait
	}

	return half + rand.N(half) // #nosec G404 -- retry jitter needs no cryptographic randomness
}

// sleep waits for d, or until ctx is done. It returns false if ctx is done first.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

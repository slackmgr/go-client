// Package client provides an HTTP client for the Slack Manager API.
//
// The client wraps [github.com/go-resty/resty/v2] with automatic retries,
// configurable connection pooling, and pluggable logging.
//
// # Basic Usage
//
//	c := client.New("https://api.example.com",
//	    client.WithAuthToken("my-token"),
//	    client.WithRetryCount(5),
//	)
//
//	if err := c.Connect(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer c.Close()
//
//	if err := c.Send(ctx, alert); err != nil {
//	    log.Fatal(err)
//	}
//
// # Configuration
//
// All configuration is supplied as [Option] functions passed to [New].
// Most options silently ignore invalid values and retain the default; see
// the docs of each option. All configuration is validated when
// [Client.Connect] is called, which rejects the remaining invalid values
// (e.g. of [WithRetryBackoff] and [WithAlertClient]).
//
// # Retry Behaviour
//
// [DefaultRetryCondition] retries on HTTP 429 (rate limit) and 5xx server
// errors, on transient connection errors and on attempts that exceed
// [WithTimeout]. Context cancellation, DNS resolution errors, permanent
// connection failures (network/host unreachable, permission denied) and TLS
// certificate verification failures are not retried, and the client never
// retries once the context of the send is done.
// Supply a custom function via [WithRetryCondition] to override this
// behaviour.
//
// When a response has a Retry-After header, as the API's 429 responses
// always do, the client waits exactly that long before retrying (up to
// [WithMaxRateLimitWait]). Other retries use exponential backoff with jitter
// ([WithRetryBackoff]). Retrying stops early when a Retry-After wait exceeds
// [WithMaxRateLimitWait], or when any wait would pass the context deadline.
// Without a context deadline, a call is limited to [WithSendTimeout] (default
// 2 minutes).
//
// # Rate Limit Pacing
//
// The client mirrors the API's per-channel token bucket rate limiter, learned
// from the RateLimit-Policy and RateLimit-* response headers, and waits before
// sending a request the API would reject with 429. See [WithRateLimitPacing].
//
// # Errors
//
// Non-2xx responses are returned as [*APIError], as are requests that rate
// limit pacing did not send ([APIError.NotSent]). Use [errors.Is] with
// [ErrRateLimited] to detect rate limiting, and [APIError.RetryAfter] to
// decide when to try again. When the context is done while waiting for a
// retry, the error wraps both the error of the previous attempt and the
// context error.
//
// # Authentication
//
// Token-based authentication is configured with [WithAuthToken] (and
// optionally [WithAuthScheme]). HTTP Basic authentication is configured
// with [WithBasicAuth]. The two methods are mutually exclusive.
//
// # Logging
//
// Implement [RequestLogger] and supply it via [WithRequestLogger] to
// integrate with your logging library. The default [NoopLogger] discards
// all log output. Ensure your implementation redacts credentials and tokens
// from request and response bodies before persisting logs.
package client

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
// Invalid values are silently ignored and the default is retained;
// all configuration is validated when [Client.Connect] is called.
//
// # Retry Behaviour
//
// [DefaultRetryCondition] retries on HTTP 429 (rate limit) and 5xx server
// errors, and on transient connection errors. Context cancellation, deadline
// exceeded, and DNS resolution errors are never retried. Supply a custom
// function via [WithRetryCondition] to override this behaviour.
//
// Before retrying a 429, the client waits exactly the time in the Retry-After
// header (up to [WithMaxRateLimitWait]). Retries of 5xx responses and
// connection errors use exponential backoff with jitter ([WithRetryBackoff]).
// Retrying stops early when the wait exceeds [WithMaxRateLimitWait] or the
// context deadline. Without a context deadline, a send is limited to
// [WithSendTimeout] (default 2 minutes).
//
// # Rate Limit Pacing
//
// The client mirrors the API's per-channel token bucket rate limiter, learned
// from the RateLimit-Policy and RateLimit-* response headers, and waits before
// sending a request the API would reject with 429. See [WithRateLimitPacing].
//
// # Errors
//
// Non-2xx responses are returned as [*APIError]. Use [errors.Is] with
// [ErrRateLimited] to detect rate limiting, and [APIError.RetryAfter] to
// decide when to try again.
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

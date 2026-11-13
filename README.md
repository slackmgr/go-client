# slackmgr/go-client

[![Go Reference](https://pkg.go.dev/badge/github.com/slackmgr/go-client.svg)](https://pkg.go.dev/github.com/slackmgr/go-client)
[![Go Report Card](https://goreportcard.com/badge/github.com/slackmgr/go-client)](https://goreportcard.com/report/github.com/slackmgr/go-client)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![CI](https://github.com/slackmgr/go-client/workflows/CI/badge.svg)](https://github.com/slackmgr/go-client/actions)

A Go HTTP client for the [Slack Manager](https://github.com/slackmgr/core) API. Wraps [resty](https://github.com/go-resty/resty) with built-in retry logic, connection pooling, and pluggable logging.

## Installation

```bash
go get github.com/slackmgr/go-client
```

Requires Go 1.26+.

## Usage

```go
import (
    "context"
    "log"

    client "github.com/slackmgr/go-client"
    "github.com/slackmgr/types"
)

ctx := context.Background()

c := client.New("https://api.example.com",
    client.WithAuthToken("my-token"),
    client.WithRetryCount(5),
)

if err := c.Connect(ctx); err != nil {
    log.Fatal(err)
}
defer c.Close()

alert := &types.Alert{
    // populate alert fields
}

if err := c.Send(ctx, alert); err != nil {
    log.Fatal(err)
}
```

Use `SendWithResponse` when you need HTTP response metadata (status code, headers, duration):

```go
meta, err := c.SendWithResponse(ctx, alert)
if err != nil {
    if meta != nil {
        // HTTP response was received but indicated an error
        log.Printf("request failed: status=%d duration=%v", meta.StatusCode, meta.Duration)
    }
    log.Fatal(err)
}
log.Printf("sent: status=%d duration=%v", meta.StatusCode, meta.Duration)
```

`*ResponseMetadata` describes the response to the last attempt, even on non-2xx status codes. It is `nil` when that attempt got no response (a network-level error). This lets callers distinguish transport failures from server-reported errors.

| Field | Type | Description |
|-------|------|-------------|
| `StatusCode` | `int` | HTTP response status code (e.g. `200`, `429`) |
| `Duration` | `time.Duration` | Round-trip time for the request |
| `Headers` | `map[string]string` | Response headers; multi-value headers joined with `", "` |

`Connect` validates the configuration (the base URL must be an absolute `http` or `https` URL), initializes the connection pool, and pings the API (with retries, limited by `WithSendTimeout` when the context has no deadline). It is safe for concurrent use. Once it has succeeded, further calls do nothing. If it fails, the client stays unconnected (`Send` and `Ping` return an error) and `Connect` can be called again. Call `Close` when finished to release idle connections.

## Migrating from the legacy client library

`SendAlert(ctx, alert)` and `SendAlerts(ctx, alerts)` are available as deprecated wrappers around `Send`, matching the method names of the legacy client library. Alert constructors such as `types.NewErrorAlert()` work the same as in the legacy library.

Client creation differs and must be updated:

| Legacy | New |
|--------|-----|
| `client, err := slacklib.NewAlertClient().Connect(ctx, baseURL, logger, opts...)` | `c := client.New(baseURL, opts...)` followed by `err := c.Connect(ctx)` |
| `slacklib.RetryCount(n)`, `RetryWaitTime(d)`, `RetryMaxWaitTime(d)` | `client.WithRetryCount(n)`, `WithRetryBackoff(initial, max)`. The default retry count is 3 (legacy: 8) |
| `logger` argument to `Connect` (`Debugf`, `Infof`, `Errorf`) | `client.WithRequestLogger(logger)` (optional; `Debugf`, `Warnf`, `Errorf`) |
| `GetMappings(ctx)` | Not available |

## Configuration

All options are provided via `With*` constructor functions.

| Option | Default | Description |
|--------|---------|-------------|
| `WithRetryCount(int)` | `3` | Maximum retries after the first attempt, for failures accepted by the retry condition (max 100, 0 disables) |
| `WithRetryCondition(func(statusCode int, err error) bool)` | `DefaultRetryCondition` | Decides whether a failed attempt is retried (default: 429, 5xx, transient connection errors, timed out attempts). `statusCode` is 0 when no response was received |
| `WithRetryBackoff(initial, max time.Duration)` | `500ms`, `3s` | Backoff for retries without `Retry-After` (5xx, connection errors, anything else the retry condition retries): starts at `initial`, doubles per retry up to `max`, each wait a random 50–100% of the backoff (initial 100ms–1min, max 100ms–5min). Not used for responses with `Retry-After`, such as the API's 429 |
| `WithMaxRateLimitWait(time.Duration)` | `1m` | Longest `Retry-After` (e.g. of a 429) the client waits for before retrying (1s–10min). Longer waits return the error right away |
| `WithRequestLogger(RequestLogger)` | `NoopLogger` | Logger for HTTP requests and errors |
| `WithRequestHeader(header, value string)` | — | Add a custom header to all requests |
| `WithAuthToken(string)` | — | Token for `Authorization` header (mutually exclusive with `WithBasicAuth`) |
| `WithAuthScheme(string)` | `"Bearer"` | Authentication scheme used with `WithAuthToken` |
| `WithBasicAuth(username, password string)` | — | HTTP Basic authentication (mutually exclusive with `WithAuthToken`) |
| `WithTimeout(time.Duration)` | `30s` | Timeout of a single HTTP request, i.e. one attempt (1s–5min). A timed out attempt is retried |
| `WithSendTimeout(time.Duration)` | `2m` | Maximum total time of `Send`/`SendWithResponse`/`Ping`/`Connect`, including retries and waits, when the context has no deadline (0 disables, 1s–1h). A context deadline takes precedence |
| `WithUserAgent(string)` | `"slack-manager-go-client/<version>"` | `User-Agent` header value. The default uses the module version from the build info (`dev` if unknown) |
| `WithMaxIdleConns(int)` | `100` | Maximum idle connections across all hosts |
| `WithMaxConnsPerHost(int)` | `10` | Maximum connections per host (max 100) |
| `WithIdleConnTimeout(time.Duration)` | `90s` | How long idle connections remain in the pool (1s–5min) |
| `WithDisableKeepAlive(bool)` | `false` | Disable HTTP keep-alive (new connection per request) |
| `WithMaxRedirects(int)` | `10` | Maximum redirects to follow (0 disables redirects, max 20). A redirect beyond the maximum is returned as an `*APIError` with its 3xx status, and not retried |
| `WithTLSConfig(*tls.Config)` | `nil` | Custom TLS configuration for mTLS, custom CAs, etc. |
| `WithAlertsEndpoint(string)` | `"alerts"` | API endpoint path for sending alerts |
| `WithPingEndpoint(string)` | `"ping"` | API endpoint path for health checks |
| `WithAlertClient(string)` | `""` | Client identifier (`Alert.Client`, e.g. the name of your monitoring tool) set on every sent alert that has none. The caller's alerts are not modified. Max 100 characters, checked by `Connect`. When empty, Slack Manager groups alerts by `Username` |

### Retry behaviour

`DefaultRetryCondition` retries on HTTP 429 (rate limit), 5xx server errors, transient connection errors, including connection refused (as while the API restarts or is redeployed), and attempts that exceed `WithTimeout`. It does **not** retry on context cancellation, DNS resolution failures, unreachable networks/hosts, permission denied, server certificate verification failures, or a server that does not speak TLS. The client never retries once the context of the send is done (cancelled or past its deadline).

The total time of a send (or `Ping`/`Connect`), including retries and waits, is limited by the context deadline, or `WithSendTimeout` (default 2 minutes) when the context has none. The defaults need no tuning: the client follows the status codes and `Retry-After` headers of the API.

Supply a custom function via `WithRetryCondition` to override this behaviour. Call `DefaultRetryCondition` from it to extend the default:

```go
client.WithRetryCondition(func(statusCode int, err error) bool {
    return statusCode == http.StatusConflict || client.DefaultRetryCondition(statusCode, err)
})
```

Wait between attempts:

- **Responses with `Retry-After`** (the API always sends it with 429): the client waits that long, plus a random extra of up to 20%, so that concurrent requests rejected together do not all retry at the same moment. `WithMaxRateLimitWait` limits this wait
- **Everything else** (5xx and 429 responses without `Retry-After`, connection errors, failures a custom condition retries): exponential backoff with jitter (`WithRetryBackoff`)

`Retry-After` values above 24 hours are treated as 24 hours. The client stops retrying early, returning the last error, when the `Retry-After` exceeds `WithMaxRateLimitWait`, or when the next wait would pass the context deadline, since the retry could not succeed anyway. If the context is done while waiting for a retry, the error wraps both the last error and the context error, so `errors.Is(err, context.Canceled)` and `errors.As(err, &apiErr)` both work.

Retries are handled by the client itself, not resty, so requests made directly on `RestyClient()` are not retried.

### Deprecated options

These still work, but will be removed in a future major version:

| Deprecated | Use instead |
|------------|-------------|
| `WithRetryWaitTime(d)`, `WithRetryMaxWaitTime(d)` | `WithRetryBackoff(initial, max)` |
| `WithRetryPolicy(func(*resty.Response, error) bool)` | `WithRetryCondition(func(statusCode int, err error) bool)` |
| `DefaultRetryPolicy` | `DefaultRetryCondition` |

### Errors

A non-2xx response (after retries) is returned as `*client.APIError`, with the status code, error message, `Retry-After` wait and number of attempts. Rate limiting (429) matches `client.ErrRateLimited`:

```go
err := c.Send(ctx, alert)

if errors.Is(err, client.ErrRateLimited) {
    var apiErr *client.APIError
    errors.As(err, &apiErr)
    // Try again after apiErr.RetryAfter
}
```

### Logging

Implement the `RequestLogger` interface to integrate with your logging library:

```go
type RequestLogger interface {
    Errorf(format string, v ...any)
    Warnf(format string, v ...any)
    Debugf(format string, v ...any)
}
```

> **Note:** The logger may receive request and response bodies. Ensure your implementation redacts credentials and tokens before persisting logs.

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.

Copyright (c) 2026 Peter Aglen

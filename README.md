# go-client

[![Go Reference](https://pkg.go.dev/badge/github.com/slackmgr/go-client.svg)](https://pkg.go.dev/github.com/slackmgr/go-client)
[![Go Report Card](https://goreportcard.com/badge/github.com/slackmgr/go-client)](https://goreportcard.com/report/github.com/slackmgr/go-client)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![CI](https://github.com/slackmgr/go-client/workflows/CI/badge.svg)](https://github.com/slackmgr/go-client/actions)

A Go HTTP client for the [Slack Manager](https://github.com/slackmgr/slack-manager) API. Wraps [resty](https://github.com/go-resty/resty) with built-in retry logic, connection pooling, and pluggable logging.

## Installation

```bash
go get github.com/slackmgr/go-client
```

Requires Go 1.25+.

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

`Connect` validates configuration, initializes the connection pool, and pings the API. It is safe for concurrent use and will only initialize once — if it fails, subsequent calls return the same error. Call `Close` when finished to release idle connections.

## Configuration

All options are provided via `With*` constructor functions.

| Option | Default | Description |
|--------|---------|-------------|
| `WithRetryCount(int)` | `3` | Number of retry attempts (max 100) |
| `WithRetryWaitTime(time.Duration)` | `500ms` | Initial wait time between retries (100ms–1min) |
| `WithRetryMaxWaitTime(time.Duration)` | `3s` | Maximum wait time between retries (100ms–5min) |
| `WithRetryPolicy(func(*resty.Response, error) bool)` | `DefaultRetryPolicy` | Custom retry condition function |
| `WithRequestLogger(RequestLogger)` | `NoopLogger` | Logger for HTTP requests and errors |
| `WithRequestHeader(header, value string)` | — | Add a custom header to all requests |
| `WithAuthToken(string)` | — | Token for `Authorization` header (mutually exclusive with `WithBasicAuth`) |
| `WithAuthScheme(string)` | `"Bearer"` | Authentication scheme used with `WithAuthToken` |
| `WithBasicAuth(username, password string)` | — | HTTP Basic authentication (mutually exclusive with `WithAuthToken`) |
| `WithTimeout(time.Duration)` | `30s` | Per-request timeout (1s–5min) |
| `WithUserAgent(string)` | `"slack-manager-go-client/1.0"` | `User-Agent` header value |
| `WithMaxIdleConns(int)` | `100` | Maximum idle connections across all hosts |
| `WithMaxConnsPerHost(int)` | `10` | Maximum connections per host (max 100) |
| `WithIdleConnTimeout(time.Duration)` | `90s` | How long idle connections remain in the pool (1s–5min) |
| `WithDisableKeepAlive(bool)` | `false` | Disable HTTP keep-alive (new connection per request) |
| `WithMaxRedirects(int)` | `10` | Maximum redirects to follow (0 disables redirects, max 20) |
| `WithTLSConfig(*tls.Config)` | `nil` | Custom TLS configuration for mTLS, custom CAs, etc. |
| `WithAlertsEndpoint(string)` | `"alerts"` | API endpoint path for sending alerts |
| `WithPingEndpoint(string)` | `"ping"` | API endpoint path for health checks |

### Retry behaviour

`DefaultRetryPolicy` retries on HTTP 429 (rate limit), 5xx server errors, and transient connection errors. It does **not** retry on context cancellation, deadline exceeded, or DNS resolution failures. `Retry-After` response headers are respected for rate-limit backoff.

Supply a custom function via `WithRetryPolicy` to override this behaviour.

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

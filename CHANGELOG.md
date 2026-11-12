# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- `Connect` can be called again after it fails, e.g. once the API is available. Previously, a failed `Connect` returned the same error forever
- Docs: the `Retry-After` header is honoured for any response that has it, not only 429; `WithRetryBackoff` waits are jittered between 50% and 100% of the backoff; corrected the `ResponseMetadata` and `APIError` docs

### Fixed
- `DefaultRetryCondition` did not retry attempts that exceeded `WithTimeout`, as their error matches `context.DeadlineExceeded`
- Rate limit pacing kept the tokens of attempts without a response (e.g. connection refused while the API restarts), so retries were delayed by the time to refill them, or not sent at all
- After a failed `Connect`, `Send` and `Ping` still sent requests
- Data race between `Connect` and concurrent `Send`, `Ping`, `Close` or `RestyClient` calls

## [0.5.0] - 2026-11-12

### Added
- `APIError`: typed error for non-2xx responses, with status code, message, `Retry-After` wait and number of attempts
- `ErrRateLimited`: matches an `APIError` with status 429 via `errors.Is`
- `WithSendTimeout(time.Duration)`: maximum total time of `Send`/`SendWithResponse`, including retries and waits, when the context has no deadline (default 2m, 0 disables). Previously, a send without a context deadline could block for several minutes
- `WithRetryBackoff(initial, max time.Duration)`: backoff for retries of 5xx responses and connection errors, replacing `WithRetryWaitTime` and `WithRetryMaxWaitTime`
- `WithRetryCondition(func(statusCode int, err error) bool)` and `DefaultRetryCondition`: retry condition without resty types, replacing `WithRetryPolicy` and `DefaultRetryPolicy`
- `WithMaxRateLimitWait(time.Duration)`: longest wait because of the rate limit, i.e. a 429's `Retry-After` or client-side pacing (default 1m, 1s–10min)
- Client-side rate limit pacing (`WithRateLimitPacing`, enabled by default): the client mirrors the API's per-channel token buckets from the `RateLimit-Policy` and `RateLimit-*` headers and waits before sending requests that would be rejected with 429. If the wait exceeds `WithMaxRateLimitWait` or the context deadline, it returns an `APIError` with `NotSent` set, without sending

### Changed
- The retry condition is only called for failed attempts (error or non-2xx response). Previously, as a resty retry condition, it was also called for successful responses
- Retries are handled by the client instead of resty. Requests made directly on `RestyClient()` are no longer retried
- The client stops retrying when the requested `Retry-After` exceeds `WithMaxRateLimitWait`, or when the next wait would pass the context deadline
- Clarified the docs of `WithRetryCount` and the retry options: backoff only applies to retries of 5xx responses and connection errors, 429 retries wait for `Retry-After`
- Connection refused is now retried by `DefaultRetryCondition`, as it is what clients typically get while the API restarts or is redeployed
- The default `User-Agent` is `slack-manager-go-client/<module version>` (from the build info, `dev` if unknown), instead of the fixed `slack-manager-go-client/1.0`
- Lint: check the format strings of `types.Logger` methods

### Deprecated
- `WithRetryWaitTime` and `WithRetryMaxWaitTime`: use `WithRetryBackoff`
- `WithRetryPolicy`: use `WithRetryCondition`
- `DefaultRetryPolicy`: use `DefaultRetryCondition`

### Fixed
- `Retry-After` waits were capped by `WithRetryMaxWaitTime` (default 3s), so the client retried before the rate limit window opened and used up its retries on certain 429s
- A `Retry-After` date in the past made the client wait the full max wait instead of retrying immediately

## [0.4.0] - 2026-11-05

### Added
- `WithAlertClient(string)`: sets `Alert.Client` (the monitoring tool or integration, used by Slack Manager for grouping, filtering and reporting) on every sent alert that has no client. The caller's alerts are never modified. Values longer than `types.MaxClientLength` (100) characters are rejected by `Connect`

### Changed
- Bump `github.com/slackmgr/types` to v0.11.0 (adds `Alert.Client`)

## [0.3.3] - 2026-11-03

### Changed

- Bump `github.com/slackmgr/types` to v0.10.0 (adds `WebhookResult` and `WebhookResponse` for webhook receiver feedback)

## [0.3.2] - 2026-11-03

### Changed

- Bump `github.com/slackmgr/types` to v0.9.0 (adds `Webhook.AfterClick` for webhook post-click state)

## [0.3.1] - 2026-10-30

### Changed

- Bump `github.com/slackmgr/types` to v0.8.0

## [0.3.0] - 2026-10-21

### Added

- Deprecated `Client.SendAlert` and `Client.SendAlerts` methods, wrapping `Client.Send`, for compatibility with the legacy client library
- README section on migrating from the legacy client library

### Changed

- Minimum Go version is now 1.26
- Update GitHub Actions to latest versions (`actions/checkout` v7.0.1, `actions/setup-go` v7.0.0, `github/codeql-action` v4.39.0, `golangci/golangci-lint-action` v9.3.0)
- CI and code scanning workflows read the Go version (major.minor) from `go.mod` and install the latest patch release (`check-latest: true`), so all jobs, including gosec and both govulncheck runs, use the same Go version
- Bump `github.com/slackmgr/types` dependency to v0.7.1
- Use `errors.AsType` in `DefaultRetryPolicy` (no behaviour change)
- Disable the `exhaustruct_v5` linter (renamed in golangci-lint 2.15), matching the existing `exhaustruct` exclusion

## [0.2.8] - 2026-05-11

### Changed

- Bump `github.com/slackmgr/types` dependency to v0.6.1

## [0.2.7] - 2026-05-08

### Changed

- Bump `github.com/slackmgr/types` dependency to v0.5.2

## [0.2.6] - 2026-05-07

### Changed

- Bump `github.com/slackmgr/types` dependency to v0.5.1

## [0.2.5] - 2026-05-06

### Changed

- Bump `github.com/slackmgr/types` dependency to v0.5.0

## [0.2.4] - 2026-04-14

### Changed

- Bump `github.com/slackmgr/types` dependency to v0.4.1

## [0.2.3] - 2026-02-26

### Changed

- Bump `github.com/slackmgr/types` dependency to v0.4.0
- Bump `golang.org/x/net` to v0.51.0

## [0.2.2] - 2026-02-23

### Added

- `SendWithResponse` method returning `*ResponseMetadata` (StatusCode, Duration, Headers) alongside the error
- `ResponseMetadata` type for inspecting HTTP response details such as status code, round-trip duration, and response headers

### Changed

- `Send` now delegates to `SendWithResponse`, removing duplicated validation and marshalling logic

## [0.2.1] - 2026-02-23

### Fixed

- Do not retry permanent connection failures in `DefaultRetryPolicy`

## [0.2.0] - 2026-02-19

### Changed

- Rename Go module from `github.com/peteraglen/slack-manager-go-client` to `github.com/slackmgr/go-client`
- Rename types dependency from `github.com/peteraglen/slack-manager-common` to `github.com/slackmgr/types` and bump to v0.3.0

## [0.1.1] - 2026-02-19

### Added

- README.md with installation, usage, configuration, and build documentation
- CHANGELOG.md
- CI workflow (GitHub Actions) with test, lint, and security scan jobs
- Package-level doc comment (`doc.go`) for pkg.go.dev
- Go doc comments on all public symbols with cross-references

### Changed

- Switch from LGPL-3.0 to MIT License
- Expand CLAUDE.md with tagging/release process and README sync guidance
- Bump `slack-manager-common` dependency to v0.2.1
- Bump `resty` to v2.17.2 and `golang.org/x/net` to v0.50.0
- Move CI workflow to `.github/workflows/` (was `workflows/`)

## [0.1.0] - 2026-01-21

### Added

- HTTP transport configuration options: `WithMaxIdleConns`, `WithMaxConnsPerHost`,
  `WithIdleConnTimeout`, `WithDisableKeepAlive`, `WithTLSConfig`, `WithMaxRedirects`
- Endpoint configuration options: `WithAlertsEndpoint`, `WithPingEndpoint`
- `Ping` method for checking API connectivity after initial connect
- `Close` method for releasing idle connections
- `RestyClient` method for advanced access to the underlying resty client
- `Retry-After` header support for rate-limit-aware retries
- Comprehensive unit tests using `httptest`
- CLAUDE.md with project guidance and build configuration

### Fixed

- Context propagation through all requests
- Error wrapping with `%w` for chain inspection
- URL sanitization to prevent credential leaking in error messages
- Options validation consolidated into `Options.Validate()`

### Changed

- Bump `slack-manager-common` dependency

## [0.0.2] - 2025-12-29

### Added

- MIT License (subsequently changed — see v0.1.x unreleased)

### Changed

- Update `slack-manager-common` dependency

## [0.0.1] - 2024-05-30

### Added

- Initial implementation: `Client`, `New`, `Connect`, `Send`
- Functional options pattern for configuration
- `DefaultRetryPolicy` with retry on 429/5xx, skip on DNS and context errors
- `RequestLogger` interface and `NoopLogger` default
- `WithRetryCount`, `WithRetryWaitTime`, `WithRetryMaxWaitTime` options
- `WithAuthToken`, `WithAuthScheme`, `WithBasicAuth` options
- `WithTimeout`, `WithUserAgent`, `WithRequestHeader` options
- `WithRequestLogger`, `WithRetryPolicy` options

[Unreleased]: https://github.com/slackmgr/go-client/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/slackmgr/go-client/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/slackmgr/go-client/compare/v0.3.3...v0.4.0
[0.3.3]: https://github.com/slackmgr/go-client/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/slackmgr/go-client/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/slackmgr/go-client/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/slackmgr/go-client/compare/v0.2.8...v0.3.0
[0.2.8]: https://github.com/slackmgr/go-client/compare/v0.2.7...v0.2.8
[0.2.7]: https://github.com/slackmgr/go-client/compare/v0.2.6...v0.2.7
[0.2.6]: https://github.com/slackmgr/go-client/compare/v0.2.5...v0.2.6
[0.2.5]: https://github.com/slackmgr/go-client/compare/v0.2.4...v0.2.5
[0.2.4]: https://github.com/slackmgr/go-client/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/slackmgr/go-client/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/slackmgr/go-client/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/slackmgr/go-client/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/slackmgr/go-client/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/slackmgr/go-client/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/slackmgr/go-client/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/slackmgr/go-client/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/slackmgr/go-client/releases/tag/v0.0.1

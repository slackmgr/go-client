# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/slackmgr/go-client/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/slackmgr/go-client/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/slackmgr/go-client/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/slackmgr/go-client/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/slackmgr/go-client/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/slackmgr/go-client/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/slackmgr/go-client/releases/tag/v0.0.1

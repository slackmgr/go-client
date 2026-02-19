# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go HTTP client library for the Slack Manager API. Wraps [resty](https://github.com/go-resty/resty) with domain-specific functionality for sending alerts. Single package (`client`) with functional options pattern for configuration.

## Build Commands

```bash
make init              # Initialize modules (go mod tidy)
make test              # Full test suite: gosec, fmt, test with race detection, vet
make lint              # Run golangci-lint
make lint-fix          # Auto-fix linting issues
make bump-common-lib   # Update types package dependency to latest
```

Run a single test:
```bash
go test -run TestName ./...
```

**IMPORTANT:** Both `make test` and `make lint` MUST pass with zero errors before committing any changes. This applies regardless of whether the errors were introduced by your changes or existed previously - all issues must be resolved before committing. Always run both commands to verify code quality.

## Keeping README.md in Sync

**After every code change, check whether `README.md` needs updating.** The README is the public-facing documentation and must always reflect the actual code.

## Tagging and Releases

### Process

1. **Update `CHANGELOG.md` first** — this is MANDATORY before creating any tag.
   - Review every commit since the last tagged commit: `git log <last-tag>..HEAD --oneline`
   - Every commit MUST be considered and represented under the correct section (`Added`, `Changed`, `Fixed`, `Removed`)
   - Add the new version section above `[Unreleased]` with today's date
   - Update the comparison links at the bottom of the file

2. **Commit the changelog:**
   ```bash
   git add CHANGELOG.md
   git commit -m "Update CHANGELOG for vX.Y.Z"
   ```

3. **Create and push the tag:**
   ```bash
   git tag vX.Y.Z
   git push origin main
   git push origin vX.Y.Z
   ```

4. **Create the GitHub release:**
   ```bash
   gh release create vX.Y.Z --repo slackmgr/go-client --title "vX.Y.Z" --notes "..."
   ```
   Use the same content as the changelog entry for the release notes.

### Versioning

Follows [Semantic Versioning](https://semver.org/):
- **Patch** (`Z`): bug fixes, CI/infra changes, documentation updates
- **Minor** (`Y`): new backwards-compatible features or functionality
- **Major** (`X`): breaking changes to the public API

### Rules

- **NEVER** create a tag without updating `CHANGELOG.md` first
- **ALWAYS** review all commits since the last tag — do not rely on memory or summaries

## Architecture

**Core Components:**
- `Client` - Main client wrapping resty.Client with Connect/Send methods
- `Options` - Functional options pattern for configuration (retries, auth, logging)
- `DefaultRetryPolicy` - Retry logic that handles 429/5xx, skips DNS and context errors
- `RequestLogger` - Interface for pluggable logging (NoopLogger default)

**Workflow:**
```go
c := client.New(baseURL, client.WithRetryCount(5), client.WithAuthToken("token"))
err := c.Connect(ctx)  // Validates via ping
err = c.Send(ctx, alerts...)
```

**Dependencies:**
- `github.com/go-resty/resty/v2` - HTTP client with retry support
- `github.com/slackmgr/types` - Shared Alert type

## Code Style

- Uses golangci-lint with strict config (see `.golangci.yaml`)
- All operations require context for cancellation support
- Errors wrapped with `fmt.Errorf("%w")` for chain inspection
- Protected headers (Content-Type, Accept) cannot be overridden

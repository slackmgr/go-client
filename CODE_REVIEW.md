# Code Review: slack-manager-go-client

**Date:** 2026-01-20
**Reviewer:** Claude Code
**Scope:** Comprehensive review of Go HTTP client library (273 lines)

---

## Executive Summary

Overall quality is solid for a focused utility library. The functional options pattern is well-implemented and the codebase follows Go conventions. However, there are **3 critical issues** that can cause runtime panics and should be fixed before production use.

---

## CRITICAL Issues

### 1. Nil Pointer Dereference in Error Handling

**Location:** `client.go:97`, `client.go:112`

When HTTP requests fail, `response` can be nil, but the code accesses `response.Request.URL`:

```go
// client.go:95-97
response, err := request.Get(path)
if err != nil {
    return fmt.Errorf("GET %s failed: %w", response.Request.URL, err)  // PANIC
}
```

Same issue exists in `post()` at line 112.

**Impact:** Network failures, connection refused, or timeouts will crash the application.

**Fix:**
```go
if err != nil {
    url := path
    if response != nil && response.Request != nil {
        url = response.Request.URL
    }
    return fmt.Errorf("GET %s failed: %w", url, err)
}
```

---

### 2. Missing `c.client` Nil Check in Send()

**Location:** `client.go:68-70, 85`

```go
func (c *Client) Send(ctx context.Context, alerts ...*common.Alert) error {
    if c == nil {
        return errors.New("alert client is nil")
    }
    // Missing: if c.client == nil check
    // ...
    return c.post(ctx, "alerts", body)  // Line 85 - panics at line 93 via c.client.R()
}
```

**Impact:** If `Send()` is called before `Connect()`, `c.client` is nil and the application panics.

**Fix:** Add check after line 70:
```go
if c.client == nil {
    return errors.New("client not connected - call Connect() first")
}
```

---

### 3. Race Condition on Client Initialization

**Location:** `client.go:41`

```go
func (c *Client) Connect(ctx context.Context) (*Client, error) {
    // ...
    c.client = resty.New()...  // Not synchronized
}
```

**Impact:** Concurrent calls to `Connect()` or calling `Send()` while `Connect()` is running creates a data race. Go's race detector will flag this.

**Fix:** Use `sync.Once` for initialization:
```go
type Client struct {
    baseURL string
    client  *resty.Client
    options *Options
    once    sync.Once
}

func (c *Client) Connect(ctx context.Context) (*Client, error) {
    var connectErr error
    c.once.Do(func() {
        if c.baseURL == "" {
            connectErr = errors.New("base URL must be set")
            return
        }
        c.client = resty.New()...
        // ... rest of initialization
    })
    if connectErr != nil {
        return nil, connectErr
    }
    return c, nil
}
```

---

## HIGH Priority Issues

### 4. Fragile DNS Error Detection

**Location:** `default_retry_policy.go:17`

```go
!strings.Contains(err.Error(), "no such host")
```

String matching is OS-dependent:
- Linux: `"no such host"`
- macOS: `"nodename nor servname provided"` or `"no such host"`
- Windows: `"getaddrinfow: No such host is known"`

**Impact:** DNS failures on non-Linux systems may trigger unnecessary retries.

**Fix:** Use proper type assertion:
```go
import "net"

var dnsErr *net.DNSError
if errors.As(err, &dnsErr) {
    return false  // Don't retry DNS errors
}
```

---

### 5. No Validation of Wait Time Relationship

**Location:** `options.go:47-60`

`WithRetryWaitTime` and `WithRetryMaxWaitTime` both validate `>= 100ms` but don't ensure `maxWaitTime >= waitTime`:

```go
client.New(url,
    WithRetryWaitTime(2*time.Second),
    WithRetryMaxWaitTime(500*time.Millisecond),  // max < initial - invalid state
)
```

**Impact:** Creates undefined retry behavior.

**Fix:** Add cross-validation in `Connect()` or document the constraint.

---

### 6. Silent Validation Failures

**Location:** `options.go:39-61`

Invalid values are silently ignored:

```go
func WithRetryCount(count int) Option {
    return func(o *Options) {
        if count >= 0 {
            o.retryCount = count
        }
        // Negative counts silently ignored
    }
}
```

Same for wait times below 100ms threshold (lines 49, 57).

**Impact:** Developers may not realize their configuration is being ignored.

**Fix Options:**
1. Return errors from option functions (breaking change)
2. Log warnings when values are rejected
3. Document the validation constraints

---

## MEDIUM Priority Issues

### 7. Code Duplication Between get() and post()

**Location:** `client.go:92-120`

The `get()` and `post()` methods share ~80% identical code structure:
- Create request with context
- Execute request
- Check for errors (same pattern)
- Check success status (same pattern)
- Format error messages (same pattern)

**Suggestion:** Extract a common `doRequest(method, path, body)` helper.

---

### 8. Awkward Connect() Return Value

**Location:** `client.go:36`

```go
func (c *Client) Connect(ctx context.Context) (*Client, error)
```

Returns the receiver pointer on success, which the caller already has.

**Suggestion:** Change to `func (c *Client) Connect(ctx context.Context) error`

---

### 9. No Nil Element Validation in Alerts Slice

**Location:** `client.go:67`

```go
func (c *Client) Send(ctx context.Context, alerts ...*common.Alert) error
```

Accepts `*common.Alert` pointers but doesn't validate individual elements for nil.

**Impact:** `json.Marshal` will encode nil as `null`, which may cause server-side issues.

---

### 10. Response Body in Error Messages

**Location:** `client.go:101, 116`

```go
return fmt.Errorf("GET %s failed with status code %d: %s",
    response.Request.URL, response.StatusCode(), getBodyErrorMessage(response))
```

**Impact:** Response body could contain sensitive data that gets logged or exposed in error chains.

**Suggestion:** Truncate or sanitize response body in error messages.

---

## LOW Priority Issues

### 11. No Upper Bound on Retry Count

**Location:** `options.go:41`

Allows arbitrarily large retry counts. With 3-second max wait, 1000 retries = potentially 50+ minutes of retrying.

---

### 12. Missing Godoc Comments

All exported functions (`New`, `Connect`, `Send`, all `With*` options) lack documentation comments.

---

### 13. Authentication Mutual Exclusivity

**Location:** `client.go:53-58`

Both BasicAuth and Token auth can be set simultaneously, but only BasicAuth wins (checked first). No warning when both are configured.

---

## Linting Configuration Notes

**File:** `.golangci.yaml`

- `dupl` linter is **disabled** - should consider enabling given the code duplication found
- `wrapcheck` is **disabled** - would help enforce error handling discipline

---

## Summary Table

| Severity | Count | Key Issues |
|----------|-------|------------|
| CRITICAL | 3 | Nil pointer panic (×2), missing client nil check |
| HIGH | 3 | DNS detection, wait time validation, silent failures |
| MEDIUM | 4 | Code duplication, awkward API, nil elements, sensitive data |
| LOW | 3 | No upper bounds, missing docs, auth exclusivity |

---

## Implementation Plan

### Phase 1: Critical Fixes (Blocking for Production)

1. **Fix nil pointer dereference in get()/post()**
   - File: `client.go`
   - Lines: 96-97, 111-112
   - Add nil checks before accessing response.Request.URL

2. **Add c.client nil check in Send()**
   - File: `client.go`
   - Line: After 70
   - Return descriptive error if client not connected

3. **Add thread-safety to Connect()**
   - File: `client.go`
   - Add `sync.Once` field to Client struct
   - Wrap initialization in `once.Do()`

### Phase 2: High Priority Fixes

4. **Fix DNS error detection**
   - File: `default_retry_policy.go`
   - Use `net.DNSError` type assertion instead of string matching

5. **Add wait time relationship validation**
   - File: `client.go` (in Connect) or `options.go`
   - Validate maxWaitTime >= waitTime

### Phase 3: Medium Priority (Optional)

6. Refactor get()/post() duplication
7. Change Connect() return signature
8. Add nil element validation in Send()

---

## Verification Steps

After implementing fixes:

```bash
# Must pass
make lint
make test

# Manual verification
# 1. Call Send() before Connect() - should return error, not panic
# 2. Simulate network failure - should not panic
# 3. Run with -race flag to verify no data races
go test -race ./...
```

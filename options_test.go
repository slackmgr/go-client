package client

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

func TestNewClientOptions(t *testing.T) {
	t.Parallel()

	opts := newClientOptions()

	if opts.retryCount != 3 {
		t.Errorf("expected retryCount=3, got %d", opts.retryCount)
	}

	if opts.retryWaitTime != 500*time.Millisecond {
		t.Errorf("expected retryWaitTime=500ms, got %v", opts.retryWaitTime)
	}

	if opts.retryMaxWaitTime != 3*time.Second {
		t.Errorf("expected retryMaxWaitTime=3s, got %v", opts.retryMaxWaitTime)
	}

	if opts.requestLogger == nil {
		t.Error("expected requestLogger to be set")
	}

	if opts.retryPolicy == nil {
		t.Error("expected retryPolicy to be set")
	}

	if opts.requestHeaders["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %s", opts.requestHeaders["Content-Type"])
	}

	if opts.requestHeaders["Accept"] != "application/json" {
		t.Errorf("expected Accept=application/json, got %s", opts.requestHeaders["Accept"])
	}

	if opts.timeout != 30*time.Second {
		t.Errorf("expected timeout=30s, got %v", opts.timeout)
	}

	if opts.userAgent != "slack-manager-go-client/1.0" {
		t.Errorf("expected userAgent=slack-manager-go-client/1.0, got %s", opts.userAgent)
	}

	if opts.maxIdleConns != 100 {
		t.Errorf("expected maxIdleConns=100, got %d", opts.maxIdleConns)
	}

	if opts.maxConnsPerHost != 10 {
		t.Errorf("expected maxConnsPerHost=10, got %d", opts.maxConnsPerHost)
	}

	if opts.idleConnTimeout != 90*time.Second {
		t.Errorf("expected idleConnTimeout=90s, got %v", opts.idleConnTimeout)
	}

	if opts.disableKeepAlive != false {
		t.Errorf("expected disableKeepAlive=false, got %v", opts.disableKeepAlive)
	}

	if opts.maxRedirects != 10 {
		t.Errorf("expected maxRedirects=10, got %d", opts.maxRedirects)
	}

	if opts.authScheme != "Bearer" {
		t.Errorf("expected authScheme=Bearer, got %s", opts.authScheme)
	}

	if opts.alertsEndpoint != "alerts" {
		t.Errorf("expected alertsEndpoint=alerts, got %s", opts.alertsEndpoint)
	}

	if opts.pingEndpoint != "ping" {
		t.Errorf("expected pingEndpoint=ping, got %s", opts.pingEndpoint)
	}

	if opts.tlsConfig != nil {
		t.Errorf("expected tlsConfig=nil, got %v", opts.tlsConfig)
	}
}

func TestWithRetryCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"valid positive", 5, 5},
		{"zero", 0, 0},
		{"negative ignored", -1, 3}, // default is 3
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithRetryCount(tt.input)(opts)

			if opts.retryCount != tt.expected {
				t.Errorf("expected retryCount=%d, got %d", tt.expected, opts.retryCount)
			}
		})
	}
}

func TestWithRetryWaitTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"valid", 200 * time.Millisecond, 200 * time.Millisecond},
		{"minimum valid", 100 * time.Millisecond, 100 * time.Millisecond},
		{"below minimum ignored", 50 * time.Millisecond, 500 * time.Millisecond}, // default is 500ms
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithRetryWaitTime(tt.input)(opts)

			if opts.retryWaitTime != tt.expected {
				t.Errorf("expected retryWaitTime=%v, got %v", tt.expected, opts.retryWaitTime)
			}
		})
	}
}

func TestWithRetryMaxWaitTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"valid", 5 * time.Second, 5 * time.Second},
		{"minimum valid", 100 * time.Millisecond, 100 * time.Millisecond},
		{"below minimum ignored", 50 * time.Millisecond, 3 * time.Second}, // default is 3s
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithRetryMaxWaitTime(tt.input)(opts)

			if opts.retryMaxWaitTime != tt.expected {
				t.Errorf("expected retryMaxWaitTime=%v, got %v", tt.expected, opts.retryMaxWaitTime)
			}
		})
	}
}

func TestWithRequestLogger(t *testing.T) {
	t.Parallel()

	t.Run("valid logger", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		logger := &NoopLogger{}
		WithRequestLogger(logger)(opts)

		if opts.requestLogger != logger {
			t.Error("expected requestLogger to be set")
		}
	})

	t.Run("nil ignored", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		originalLogger := opts.requestLogger
		WithRequestLogger(nil)(opts)

		if opts.requestLogger != originalLogger {
			t.Error("nil logger should be ignored")
		}
	})
}

func TestWithRetryPolicy(t *testing.T) {
	t.Parallel()

	t.Run("valid policy", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		policy := func(_ *resty.Response, _ error) bool { return true }
		WithRetryPolicy(policy)(opts)

		if opts.retryPolicy == nil {
			t.Error("expected retryPolicy to be set")
		}
	})

	t.Run("nil ignored", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		originalPolicy := opts.retryPolicy
		WithRetryPolicy(nil)(opts)

		if opts.retryPolicy == nil {
			t.Error("nil policy should be ignored")
		}

		// Check that the policy is still the original (DefaultRetryPolicy)
		if originalPolicy == nil {
			t.Error("original policy should not be nil")
		}
	})
}

func TestWithRequestHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		header        string
		value         string
		expectIgnored bool
	}{
		{"valid header", "X-Custom", "value", false},
		{"empty header ignored", "", "value", true},
		{"whitespace header ignored", "   ", "value", true},
		{"Content-Type protected", "Content-Type", "text/plain", true},
		{"content-type protected (case insensitive)", "content-type", "text/plain", true},
		{"Accept protected", "Accept", "text/plain", true},
		{"accept protected (case insensitive)", "ACCEPT", "text/plain", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			originalContentType := opts.requestHeaders["Content-Type"]
			originalAccept := opts.requestHeaders["Accept"]
			originalLen := len(opts.requestHeaders)

			WithRequestHeader(tt.header, tt.value)(opts)

			if tt.expectIgnored {
				// Verify protected headers weren't changed
				if opts.requestHeaders["Content-Type"] != originalContentType {
					t.Error("Content-Type should not be changed")
				}
				if opts.requestHeaders["Accept"] != originalAccept {
					t.Error("Accept should not be changed")
				}
				if tt.header == "" || tt.header == "   " {
					if len(opts.requestHeaders) != originalLen {
						t.Error("empty header should not add to headers")
					}
				}
			} else if opts.requestHeaders[tt.header] != tt.value {
				t.Errorf("expected header %s=%s, got %s", tt.header, tt.value, opts.requestHeaders[tt.header])
			}
		})
	}
}

func TestWithBasicAuth(t *testing.T) {
	t.Parallel()

	opts := newClientOptions()
	WithBasicAuth("user", "pass")(opts)

	if opts.basicAuthUsername != "user" {
		t.Errorf("expected username=user, got %s", opts.basicAuthUsername)
	}

	if opts.basicAuthPassword != "pass" {
		t.Errorf("expected password=pass, got %s", opts.basicAuthPassword)
	}
}

func TestWithAuthScheme(t *testing.T) {
	t.Parallel()

	opts := newClientOptions()
	WithAuthScheme("Bearer")(opts)

	if opts.authScheme != "Bearer" {
		t.Errorf("expected scheme=Bearer, got %s", opts.authScheme)
	}
}

func TestWithAuthToken(t *testing.T) {
	t.Parallel()

	opts := newClientOptions()
	WithAuthToken("my-token")(opts)

	if opts.authToken != "my-token" {
		t.Errorf("expected token=my-token, got %s", opts.authToken)
	}
}

func TestWithTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"valid", 10 * time.Second, 10 * time.Second},
		{"minimum valid", 1 * time.Second, 1 * time.Second},
		{"maximum valid", 5 * time.Minute, 5 * time.Minute},
		{"below minimum ignored", 500 * time.Millisecond, 30 * time.Second},
		{"above maximum ignored", 10 * time.Minute, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithTimeout(tt.input)(opts)

			if opts.timeout != tt.expected {
				t.Errorf("expected timeout=%v, got %v", tt.expected, opts.timeout)
			}
		})
	}
}

func TestWithUserAgent(t *testing.T) {
	t.Parallel()

	t.Run("valid user agent", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		WithUserAgent("custom-agent/2.0")(opts)

		if opts.userAgent != "custom-agent/2.0" {
			t.Errorf("expected userAgent=custom-agent/2.0, got %s", opts.userAgent)
		}
	})

	t.Run("empty ignored", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		WithUserAgent("")(opts)

		if opts.userAgent != "slack-manager-go-client/1.0" {
			t.Errorf("expected default userAgent, got %s", opts.userAgent)
		}
	})
}

func TestWithMaxIdleConns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"valid", 50, 50},
		{"minimum valid", 1, 1},
		{"zero ignored", 0, 100},
		{"negative ignored", -1, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithMaxIdleConns(tt.input)(opts)

			if opts.maxIdleConns != tt.expected {
				t.Errorf("expected maxIdleConns=%d, got %d", tt.expected, opts.maxIdleConns)
			}
		})
	}
}

func TestWithMaxConnsPerHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"valid", 20, 20},
		{"minimum valid", 1, 1},
		{"maximum valid", 100, 100},
		{"zero ignored", 0, 10},
		{"negative ignored", -1, 10},
		{"above maximum ignored", 101, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithMaxConnsPerHost(tt.input)(opts)

			if opts.maxConnsPerHost != tt.expected {
				t.Errorf("expected maxConnsPerHost=%d, got %d", tt.expected, opts.maxConnsPerHost)
			}
		})
	}
}

func TestWithIdleConnTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"valid", 60 * time.Second, 60 * time.Second},
		{"minimum valid", 1 * time.Second, 1 * time.Second},
		{"maximum valid", 5 * time.Minute, 5 * time.Minute},
		{"below minimum ignored", 500 * time.Millisecond, 90 * time.Second},
		{"above maximum ignored", 10 * time.Minute, 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithIdleConnTimeout(tt.input)(opts)

			if opts.idleConnTimeout != tt.expected {
				t.Errorf("expected idleConnTimeout=%v, got %v", tt.expected, opts.idleConnTimeout)
			}
		})
	}
}

func TestWithDisableKeepAlive(t *testing.T) {
	t.Parallel()

	t.Run("disable keep-alive", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		WithDisableKeepAlive(true)(opts)

		if opts.disableKeepAlive != true {
			t.Errorf("expected disableKeepAlive=true, got %v", opts.disableKeepAlive)
		}
	})

	t.Run("enable keep-alive", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		opts.disableKeepAlive = true
		WithDisableKeepAlive(false)(opts)

		if opts.disableKeepAlive != false {
			t.Errorf("expected disableKeepAlive=false, got %v", opts.disableKeepAlive)
		}
	})
}

func TestWithMaxRedirects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"valid", 5, 5},
		{"zero (disable redirects)", 0, 0},
		{"maximum valid", 20, 20},
		{"negative ignored", -1, 10},
		{"above maximum ignored", 21, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithMaxRedirects(tt.input)(opts)

			if opts.maxRedirects != tt.expected {
				t.Errorf("expected maxRedirects=%d, got %d", tt.expected, opts.maxRedirects)
			}
		})
	}
}

func TestOptionsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		modify    func(*Options)
		wantError string
	}{
		{
			name:      "valid defaults",
			modify:    func(_ *Options) {},
			wantError: "",
		},
		{
			name:      "negative retryCount",
			modify:    func(o *Options) { o.retryCount = -1 },
			wantError: "retryCount must be non-negative",
		},
		{
			name:      "retryCount exceeds max",
			modify:    func(o *Options) { o.retryCount = 101 },
			wantError: "retryCount must not exceed 100",
		},
		{
			name:      "retryWaitTime below minimum",
			modify:    func(o *Options) { o.retryWaitTime = 50 * time.Millisecond },
			wantError: "retryWaitTime must be at least 100ms",
		},
		{
			name:      "retryWaitTime exceeds max",
			modify:    func(o *Options) { o.retryWaitTime = 2 * time.Minute },
			wantError: "retryWaitTime must not exceed 1m0s",
		},
		{
			name:      "retryMaxWaitTime below minimum",
			modify:    func(o *Options) { o.retryMaxWaitTime = 50 * time.Millisecond },
			wantError: "retryMaxWaitTime must be at least 100ms",
		},
		{
			name:      "retryMaxWaitTime exceeds max",
			modify:    func(o *Options) { o.retryMaxWaitTime = 6 * time.Minute },
			wantError: "retryMaxWaitTime must not exceed 5m0s",
		},
		{
			name: "retryMaxWaitTime less than retryWaitTime",
			modify: func(o *Options) {
				o.retryWaitTime = 1 * time.Second
				o.retryMaxWaitTime = 500 * time.Millisecond
			},
			wantError: "retryMaxWaitTime (500ms) must be greater than or equal to retryWaitTime (1s)",
		},
		{
			name:      "nil requestLogger",
			modify:    func(o *Options) { o.requestLogger = nil },
			wantError: "requestLogger must not be nil",
		},
		{
			name:      "nil retryPolicy",
			modify:    func(o *Options) { o.retryPolicy = nil },
			wantError: "retryPolicy must not be nil",
		},
		{
			name: "both auth methods",
			modify: func(o *Options) {
				o.basicAuthUsername = "user"
				o.authToken = "token"
			},
			wantError: "cannot use both basic auth and token auth - choose one",
		},
		{
			name:      "timeout below minimum",
			modify:    func(o *Options) { o.timeout = 500 * time.Millisecond },
			wantError: "timeout must be at least 1s",
		},
		{
			name:      "timeout exceeds max",
			modify:    func(o *Options) { o.timeout = 10 * time.Minute },
			wantError: "timeout must not exceed 5m0s",
		},
		{
			name:      "empty userAgent",
			modify:    func(o *Options) { o.userAgent = "" },
			wantError: "userAgent must not be empty",
		},
		{
			name:      "maxIdleConns below minimum",
			modify:    func(o *Options) { o.maxIdleConns = 0 },
			wantError: "maxIdleConns must be at least 1",
		},
		{
			name:      "maxConnsPerHost below minimum",
			modify:    func(o *Options) { o.maxConnsPerHost = 0 },
			wantError: "maxConnsPerHost must be at least 1",
		},
		{
			name:      "maxConnsPerHost exceeds max",
			modify:    func(o *Options) { o.maxConnsPerHost = 101 },
			wantError: "maxConnsPerHost must not exceed 100",
		},
		{
			name:      "idleConnTimeout below minimum",
			modify:    func(o *Options) { o.idleConnTimeout = 500 * time.Millisecond },
			wantError: "idleConnTimeout must be at least 1s",
		},
		{
			name:      "idleConnTimeout exceeds max",
			modify:    func(o *Options) { o.idleConnTimeout = 10 * time.Minute },
			wantError: "idleConnTimeout must not exceed 5m0s",
		},
		{
			name:      "maxRedirects negative",
			modify:    func(o *Options) { o.maxRedirects = -1 },
			wantError: "maxRedirects must be non-negative",
		},
		{
			name:      "maxRedirects exceeds max",
			modify:    func(o *Options) { o.maxRedirects = 21 },
			wantError: "maxRedirects must not exceed 20",
		},
		{
			name:      "empty alertsEndpoint",
			modify:    func(o *Options) { o.alertsEndpoint = "" },
			wantError: "alertsEndpoint must not be empty",
		},
		{
			name:      "empty pingEndpoint",
			modify:    func(o *Options) { o.pingEndpoint = "" },
			wantError: "pingEndpoint must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			tt.modify(opts)

			err := opts.Validate()

			if tt.wantError == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantError)
				} else if err.Error() != tt.wantError {
					t.Errorf("expected error %q, got %q", tt.wantError, err.Error())
				}
			}
		})
	}
}

func TestWithTLSConfig(t *testing.T) {
	t.Parallel()

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		WithTLSConfig(tlsConfig)(opts)

		if opts.tlsConfig != tlsConfig {
			t.Error("expected tlsConfig to be set")
		}
	})

	t.Run("nil ignored", func(t *testing.T) {
		t.Parallel()

		opts := newClientOptions()
		opts.tlsConfig = &tls.Config{}
		originalConfig := opts.tlsConfig
		WithTLSConfig(nil)(opts)

		if opts.tlsConfig != originalConfig {
			t.Error("nil config should be ignored")
		}
	})
}

func TestWithAlertsEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid endpoint", "v2/alerts", "v2/alerts"},
		{"empty ignored", "", "alerts"},
		{"whitespace ignored", "   ", "alerts"},
		{"whitespace trimmed", "  api/alerts  ", "api/alerts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithAlertsEndpoint(tt.input)(opts)

			if opts.alertsEndpoint != tt.expected {
				t.Errorf("expected alertsEndpoint=%s, got %s", tt.expected, opts.alertsEndpoint)
			}
		})
	}
}

func TestWithPingEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid endpoint", "health", "health"},
		{"empty ignored", "", "ping"},
		{"whitespace ignored", "   ", "ping"},
		{"whitespace trimmed", "  healthz  ", "healthz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := newClientOptions()
			WithPingEndpoint(tt.input)(opts)

			if opts.pingEndpoint != tt.expected {
				t.Errorf("expected pingEndpoint=%s, got %s", tt.expected, opts.pingEndpoint)
			}
		})
	}
}

func TestWithRequestHeader_ValueTrimmed(t *testing.T) {
	t.Parallel()

	opts := newClientOptions()
	WithRequestHeader("X-Custom", "  value with spaces  ")(opts)

	if opts.requestHeaders["X-Custom"] != "value with spaces" {
		t.Errorf("expected trimmed value, got %q", opts.requestHeaders["X-Custom"])
	}
}

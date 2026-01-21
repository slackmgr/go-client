package client

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	maxRetryCount       = 100
	minRetryWaitTime    = 100 * time.Millisecond
	maxRetryWaitTime    = 1 * time.Minute
	minRetryMaxWaitTime = 100 * time.Millisecond
	maxRetryMaxWaitTime = 5 * time.Minute

	defaultTimeout         = 30 * time.Second
	minTimeout             = 1 * time.Second
	maxTimeout             = 5 * time.Minute
	defaultUserAgent       = "slack-manager-go-client/1.0"
	defaultMaxIdleConns    = 100
	defaultMaxConnsPerHost = 10
	maxMaxConnsPerHost     = 100
	defaultIdleConnTimeout = 90 * time.Second
	minIdleConnTimeout     = 1 * time.Second
	maxIdleConnTimeout     = 5 * time.Minute
	defaultMaxRedirects    = 10
	maxMaxRedirects        = 20
	defaultAuthScheme      = "Bearer"
	defaultAlertsEndpoint  = "alerts"
	defaultPingEndpoint    = "ping"
)

// Option is a functional option for configuring a Client.
type Option func(*Options)

type Options struct {
	retryCount        int
	retryWaitTime     time.Duration
	retryMaxWaitTime  time.Duration
	requestLogger     RequestLogger
	retryPolicy       func(*resty.Response, error) bool
	requestHeaders    map[string]string
	basicAuthUsername string
	basicAuthPassword string
	authScheme        string
	authToken         string
	timeout           time.Duration
	userAgent         string
	maxIdleConns      int
	maxConnsPerHost   int
	idleConnTimeout   time.Duration
	disableKeepAlive  bool
	maxRedirects      int
	tlsConfig         *tls.Config
	alertsEndpoint    string
	pingEndpoint      string
}

func newClientOptions() *Options {
	return &Options{
		retryCount:       3,
		retryWaitTime:    500 * time.Millisecond,
		retryMaxWaitTime: 3 * time.Second,
		requestLogger:    &NoopLogger{},
		retryPolicy:      DefaultRetryPolicy,
		requestHeaders: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		},
		timeout:          defaultTimeout,
		userAgent:        defaultUserAgent,
		maxIdleConns:     defaultMaxIdleConns,
		maxConnsPerHost:  defaultMaxConnsPerHost,
		idleConnTimeout:  defaultIdleConnTimeout,
		disableKeepAlive: false,
		maxRedirects:     defaultMaxRedirects,
		authScheme:       defaultAuthScheme,
		alertsEndpoint:   defaultAlertsEndpoint,
		pingEndpoint:     defaultPingEndpoint,
	}
}

// WithRetryCount sets the number of retry attempts for failed requests.
// Maximum allowed is 100. Default is 3.
//
// Note: Invalid values (negative) are silently ignored and the default is retained.
// Use Options.Validate() after Connect() to verify configuration.
func WithRetryCount(count int) Option {
	return func(o *Options) {
		if count >= 0 {
			o.retryCount = count
		}
	}
}

// WithRetryWaitTime sets the initial wait time between retries.
// Minimum is 100ms, maximum is 1 minute. Default is 500ms.
//
// Note: Values outside the valid range are silently ignored and the default is retained.
func WithRetryWaitTime(waitTime time.Duration) Option {
	return func(o *Options) {
		if waitTime >= 100*time.Millisecond {
			o.retryWaitTime = waitTime
		}
	}
}

// WithRetryMaxWaitTime sets the maximum wait time between retries.
// Minimum is 100ms, maximum is 5 minutes. Default is 3 seconds.
// Must be >= retryWaitTime (validated at Connect time).
//
// Note: Values outside the valid range are silently ignored and the default is retained.
func WithRetryMaxWaitTime(maxWaitTime time.Duration) Option {
	return func(o *Options) {
		if maxWaitTime >= 100*time.Millisecond {
			o.retryMaxWaitTime = maxWaitTime
		}
	}
}

// WithRequestLogger sets the logger for HTTP request logging.
// Default is NoopLogger (discards all logs).
//
// Security note: The logger may receive sensitive data in request/response bodies.
// Ensure your logger implementation sanitizes or redacts credentials, tokens,
// and other sensitive information before persisting logs.
//
// Note: Nil values are silently ignored and the default is retained.
func WithRequestLogger(logger RequestLogger) Option {
	return func(o *Options) {
		if logger != nil {
			o.requestLogger = logger
		}
	}
}

// WithRetryPolicy sets a custom retry policy function.
// Default is DefaultRetryPolicy which retries on 429, 5xx, and connection errors
// (excluding context cancellation, deadline exceeded, and DNS errors).
//
// Note: Nil values are silently ignored and the default is retained.
func WithRetryPolicy(policy func(*resty.Response, error) bool) Option {
	return func(o *Options) {
		if policy != nil {
			o.retryPolicy = policy
		}
	}
}

// WithRequestHeader adds a custom header to all requests.
// Both header name and value are trimmed of leading/trailing whitespace.
//
// Note: Empty header names and attempts to override protected headers
// (Content-Type, Accept) are silently ignored.
func WithRequestHeader(header, value string) Option {
	return func(o *Options) {
		header = strings.TrimSpace(header)
		value = strings.TrimSpace(value)

		if header == "" || strings.EqualFold(header, "Content-Type") || strings.EqualFold(header, "Accept") {
			return
		}

		o.requestHeaders[header] = value
	}
}

// WithBasicAuth configures HTTP Basic Authentication.
// Cannot be used together with WithAuthToken.
func WithBasicAuth(username, password string) Option {
	return func(o *Options) {
		o.basicAuthUsername = username
		o.basicAuthPassword = password
	}
}

// WithAuthScheme sets the authentication scheme (e.g., "Bearer").
// Used together with WithAuthToken.
func WithAuthScheme(scheme string) Option {
	return func(o *Options) {
		o.authScheme = scheme
	}
}

// WithAuthToken sets the authentication token.
// Cannot be used together with WithBasicAuth.
func WithAuthToken(token string) Option {
	return func(o *Options) {
		o.authToken = token
	}
}

// WithTimeout sets the overall request timeout.
// Minimum is 1 second, maximum is 5 minutes. Default is 30 seconds.
//
// Note: Values outside the valid range are silently ignored and the default is retained.
func WithTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		if timeout >= minTimeout && timeout <= maxTimeout {
			o.timeout = timeout
		}
	}
}

// WithUserAgent sets the User-Agent header for all requests.
// Default is "slack-manager-go-client/1.0".
//
// Note: Empty values are silently ignored and the default is retained.
func WithUserAgent(userAgent string) Option {
	return func(o *Options) {
		if userAgent != "" {
			o.userAgent = userAgent
		}
	}
}

// WithMaxIdleConns sets the maximum number of idle connections across all hosts.
// Minimum is 1. Default is 100.
//
// Note: Values less than 1 are silently ignored and the default is retained.
func WithMaxIdleConns(n int) Option {
	return func(o *Options) {
		if n >= 1 {
			o.maxIdleConns = n
		}
	}
}

// WithMaxConnsPerHost sets the maximum number of connections per host.
// Minimum is 1, maximum is 100. Default is 10.
//
// Note: Values outside the valid range are silently ignored and the default is retained.
func WithMaxConnsPerHost(n int) Option {
	return func(o *Options) {
		if n >= 1 && n <= maxMaxConnsPerHost {
			o.maxConnsPerHost = n
		}
	}
}

// WithIdleConnTimeout sets how long idle connections remain in the pool.
// Minimum is 1 second, maximum is 5 minutes. Default is 90 seconds.
//
// Note: Values outside the valid range are silently ignored and the default is retained.
func WithIdleConnTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		if timeout >= minIdleConnTimeout && timeout <= maxIdleConnTimeout {
			o.idleConnTimeout = timeout
		}
	}
}

// WithDisableKeepAlive disables HTTP keep-alive connections.
// When true, a new connection is created for each request.
// Default is false (keep-alive enabled).
func WithDisableKeepAlive(disable bool) Option {
	return func(o *Options) {
		o.disableKeepAlive = disable
	}
}

// WithMaxRedirects sets the maximum number of redirects to follow.
// Use 0 to disable redirects. Maximum is 20. Default is 10.
//
// Note: Negative values or values > 20 are silently ignored and the default is retained.
func WithMaxRedirects(n int) Option {
	return func(o *Options) {
		if n >= 0 && n <= maxMaxRedirects {
			o.maxRedirects = n
		}
	}
}

// WithTLSConfig sets custom TLS configuration for HTTPS connections.
// Use this for custom CA certificates, mutual TLS (mTLS), or TLS version control.
// Default is nil (uses Go's default TLS configuration).
//
// Note: Nil values are silently ignored.
func WithTLSConfig(config *tls.Config) Option {
	return func(o *Options) {
		if config != nil {
			o.tlsConfig = config
		}
	}
}

// WithAlertsEndpoint sets the API endpoint path for sending alerts.
// Default is "alerts".
//
// Note: Empty values are silently ignored and the default is retained.
func WithAlertsEndpoint(endpoint string) Option {
	return func(o *Options) {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" {
			o.alertsEndpoint = endpoint
		}
	}
}

// WithPingEndpoint sets the API endpoint path for health checks.
// Default is "ping".
//
// Note: Empty values are silently ignored and the default is retained.
func WithPingEndpoint(endpoint string) Option {
	return func(o *Options) {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" {
			o.pingEndpoint = endpoint
		}
	}
}

// Validate checks all options fields for validity and returns an error if any are invalid.
func (o *Options) Validate() error {
	if o.retryCount < 0 {
		return errors.New("retryCount must be non-negative")
	}

	if o.retryCount > maxRetryCount {
		return fmt.Errorf("retryCount must not exceed %d", maxRetryCount)
	}

	if o.retryWaitTime < minRetryWaitTime {
		return fmt.Errorf("retryWaitTime must be at least %v", minRetryWaitTime)
	}

	if o.retryWaitTime > maxRetryWaitTime {
		return fmt.Errorf("retryWaitTime must not exceed %v", maxRetryWaitTime)
	}

	if o.retryMaxWaitTime < minRetryMaxWaitTime {
		return fmt.Errorf("retryMaxWaitTime must be at least %v", minRetryMaxWaitTime)
	}

	if o.retryMaxWaitTime > maxRetryMaxWaitTime {
		return fmt.Errorf("retryMaxWaitTime must not exceed %v", maxRetryMaxWaitTime)
	}

	if o.retryMaxWaitTime < o.retryWaitTime {
		return fmt.Errorf("retryMaxWaitTime (%v) must be greater than or equal to retryWaitTime (%v)", o.retryMaxWaitTime, o.retryWaitTime)
	}

	if o.requestLogger == nil {
		return errors.New("requestLogger must not be nil")
	}

	if o.retryPolicy == nil {
		return errors.New("retryPolicy must not be nil")
	}

	if o.basicAuthUsername != "" && o.authToken != "" {
		return errors.New("cannot use both basic auth and token auth - choose one")
	}

	if o.timeout < minTimeout {
		return fmt.Errorf("timeout must be at least %v", minTimeout)
	}

	if o.timeout > maxTimeout {
		return fmt.Errorf("timeout must not exceed %v", maxTimeout)
	}

	if o.userAgent == "" {
		return errors.New("userAgent must not be empty")
	}

	if o.maxIdleConns < 1 {
		return errors.New("maxIdleConns must be at least 1")
	}

	if o.maxConnsPerHost < 1 {
		return errors.New("maxConnsPerHost must be at least 1")
	}

	if o.maxConnsPerHost > maxMaxConnsPerHost {
		return fmt.Errorf("maxConnsPerHost must not exceed %d", maxMaxConnsPerHost)
	}

	if o.idleConnTimeout < minIdleConnTimeout {
		return fmt.Errorf("idleConnTimeout must be at least %v", minIdleConnTimeout)
	}

	if o.idleConnTimeout > maxIdleConnTimeout {
		return fmt.Errorf("idleConnTimeout must not exceed %v", maxIdleConnTimeout)
	}

	if o.maxRedirects < 0 {
		return errors.New("maxRedirects must be non-negative")
	}

	if o.maxRedirects > maxMaxRedirects {
		return fmt.Errorf("maxRedirects must not exceed %d", maxMaxRedirects)
	}

	if o.alertsEndpoint == "" {
		return errors.New("alertsEndpoint must not be empty")
	}

	if o.pingEndpoint == "" {
		return errors.New("pingEndpoint must not be empty")
	}

	return nil
}

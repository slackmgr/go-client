package client

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-resty/resty/v2"
	"github.com/slackmgr/types"
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

// Option is a functional option for configuring a [Client].
type Option func(*Options)

// Options holds the configuration for a [Client]. Use [Option] functions
// such as [WithRetryCount] or [WithAuthToken] to customise the defaults.
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
	alertClient       string
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
// The default is 3. The maximum allowed value is 100. Negative values are
// silently ignored and the default is retained.
func WithRetryCount(count int) Option {
	return func(o *Options) {
		if count >= 0 {
			o.retryCount = count
		}
	}
}

// WithRetryWaitTime sets the initial wait time between retries. The default
// is 500ms. Valid range is 100ms–1 minute. Values outside this range are
// silently ignored and the default is retained.
func WithRetryWaitTime(waitTime time.Duration) Option {
	return func(o *Options) {
		if waitTime >= 100*time.Millisecond {
			o.retryWaitTime = waitTime
		}
	}
}

// WithRetryMaxWaitTime sets the maximum wait time between retries. The
// default is 3 seconds. Valid range is 100ms–5 minutes. Must be greater
// than or equal to [WithRetryWaitTime]; this constraint is validated when
// [Client.Connect] is called. Values outside the range are silently ignored.
func WithRetryMaxWaitTime(maxWaitTime time.Duration) Option {
	return func(o *Options) {
		if maxWaitTime >= 100*time.Millisecond {
			o.retryMaxWaitTime = maxWaitTime
		}
	}
}

// WithRequestLogger sets the logger for HTTP request and error logging. The
// default is [NoopLogger], which discards all output. Nil values are silently
// ignored and the default is retained.
//
// The logger may receive request and response bodies. Ensure your
// implementation redacts credentials and tokens before persisting logs.
func WithRequestLogger(logger RequestLogger) Option {
	return func(o *Options) {
		if logger != nil {
			o.requestLogger = logger
		}
	}
}

// WithRetryPolicy sets a custom function that decides whether a failed
// request should be retried. The default is [DefaultRetryPolicy], which
// retries on 429, 5xx, and transient connection errors. Nil values are
// silently ignored and the default is retained.
func WithRetryPolicy(policy func(*resty.Response, error) bool) Option {
	return func(o *Options) {
		if policy != nil {
			o.retryPolicy = policy
		}
	}
}

// WithRequestHeader adds a custom header to all requests. Both the header
// name and value are trimmed of leading and trailing whitespace. Empty
// header names and attempts to override the protected Content-Type and
// Accept headers are silently ignored.
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

// WithBasicAuth configures HTTP Basic authentication. Mutually exclusive
// with [WithAuthToken]; supplying both is rejected when [Client.Connect]
// is called.
func WithBasicAuth(username, password string) Option {
	return func(o *Options) {
		o.basicAuthUsername = username
		o.basicAuthPassword = password
	}
}

// WithAuthScheme sets the authentication scheme used with [WithAuthToken].
// The default is "Bearer".
func WithAuthScheme(scheme string) Option {
	return func(o *Options) {
		o.authScheme = scheme
	}
}

// WithAuthToken sets the token sent in the Authorization header. Mutually
// exclusive with [WithBasicAuth]; supplying both is rejected when
// [Client.Connect] is called.
func WithAuthToken(token string) Option {
	return func(o *Options) {
		o.authToken = token
	}
}

// WithTimeout sets the per-request timeout. The default is 30 seconds.
// Valid range is 1 second–5 minutes. Values outside this range are silently
// ignored and the default is retained.
func WithTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		if timeout >= minTimeout && timeout <= maxTimeout {
			o.timeout = timeout
		}
	}
}

// WithUserAgent sets the User-Agent header sent with every request. The
// default is "slack-manager-go-client/1.0". Empty values are silently
// ignored and the default is retained.
func WithUserAgent(userAgent string) Option {
	return func(o *Options) {
		if userAgent != "" {
			o.userAgent = userAgent
		}
	}
}

// WithMaxIdleConns sets the maximum number of idle (keep-alive) connections
// across all hosts. The default is 100. Values less than 1 are silently
// ignored and the default is retained.
func WithMaxIdleConns(n int) Option {
	return func(o *Options) {
		if n >= 1 {
			o.maxIdleConns = n
		}
	}
}

// WithMaxConnsPerHost sets the maximum total (idle and active) connections
// per host. The default is 10. Valid range is 1–100. Values outside this
// range are silently ignored and the default is retained.
func WithMaxConnsPerHost(n int) Option {
	return func(o *Options) {
		if n >= 1 && n <= maxMaxConnsPerHost {
			o.maxConnsPerHost = n
		}
	}
}

// WithIdleConnTimeout sets how long an idle keep-alive connection remains
// in the pool before being closed. The default is 90 seconds. Valid range
// is 1 second–5 minutes. Values outside this range are silently ignored and
// the default is retained.
func WithIdleConnTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		if timeout >= minIdleConnTimeout && timeout <= maxIdleConnTimeout {
			o.idleConnTimeout = timeout
		}
	}
}

// WithDisableKeepAlive controls whether HTTP keep-alive is disabled. When
// true, a new connection is opened for each request. The default is false.
func WithDisableKeepAlive(disable bool) Option {
	return func(o *Options) {
		o.disableKeepAlive = disable
	}
}

// WithMaxRedirects sets the maximum number of redirects to follow. Use 0
// to disable redirects entirely. The default is 10. The maximum is 20.
// Negative values or values greater than 20 are silently ignored and the
// default is retained.
func WithMaxRedirects(n int) Option {
	return func(o *Options) {
		if n >= 0 && n <= maxMaxRedirects {
			o.maxRedirects = n
		}
	}
}

// WithTLSConfig sets a custom TLS configuration for HTTPS connections. Use
// this for custom CA certificates, mutual TLS (mTLS), or TLS version
// constraints. The default is nil, which uses Go's default TLS settings.
// Nil values are silently ignored.
func WithTLSConfig(config *tls.Config) Option {
	return func(o *Options) {
		if config != nil {
			o.tlsConfig = config
		}
	}
}

// WithAlertsEndpoint sets the API endpoint path used when sending alerts.
// The default is "alerts". Empty and whitespace-only values are silently
// ignored and the default is retained.
func WithAlertsEndpoint(endpoint string) Option {
	return func(o *Options) {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" {
			o.alertsEndpoint = endpoint
		}
	}
}

// WithPingEndpoint sets the API endpoint path used for health checks. The
// default is "ping". Empty and whitespace-only values are silently ignored
// and the default is retained.
func WithPingEndpoint(endpoint string) Option {
	return func(o *Options) {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" {
			o.pingEndpoint = endpoint
		}
	}
}

// WithAlertClient sets the client identifier ([types.Alert.Client]) of every sent alert that has none, e.g. the
// name of the monitoring tool or integration that uses this client. Alerts with a non-empty Client are sent
// unchanged, and the caller's alerts are never modified. The value is trimmed; it must not exceed
// [types.MaxClientLength] characters, which is checked when [Client.Connect] is called. The default is empty,
// in which case the Slack Manager falls back to the alert's Username (see [types.Alert.EffectiveClient]).
func WithAlertClient(client string) Option {
	return func(o *Options) {
		o.alertClient = strings.TrimSpace(client)
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

	if utf8.RuneCountInString(o.alertClient) > types.MaxClientLength {
		return fmt.Errorf("alertClient must not exceed %d characters", types.MaxClientLength)
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

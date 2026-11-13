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

	defaultSendTimeout = 2 * time.Minute
	minSendTimeout     = 1 * time.Second
	maxSendTimeout     = 1 * time.Hour

	defaultMaxRateLimitWait = 1 * time.Minute
	minMaxRateLimitWait     = 1 * time.Second
	maxMaxRateLimitWait     = 10 * time.Minute

	defaultTimeout         = 30 * time.Second
	minTimeout             = 1 * time.Second
	maxTimeout             = 5 * time.Minute
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
	maxRateLimitWait  time.Duration
	sendTimeout       time.Duration
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
		maxRateLimitWait: defaultMaxRateLimitWait,
		sendTimeout:      defaultSendTimeout,
		requestLogger:    &NoopLogger{},
		retryPolicy:      retryConditionPolicy(DefaultRetryCondition),
		requestHeaders: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		},
		timeout:          defaultTimeout,
		userAgent:        defaultUserAgent(),
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

// WithRetryCount sets the maximum number of retries after the first attempt.
// A failed attempt is retried when the retry condition ([WithRetryCondition])
// says so; by default on 429, 5xx and transient connection errors. 0 disables
// retries. The default is 3.
//
// Negative values are silently ignored. Values above 100 are rejected by
// [Client.Connect].
func WithRetryCount(count int) Option {
	return func(o *Options) {
		if count >= 0 {
			o.retryCount = count
		}
	}
}

// WithRetryBackoff sets the backoff between retries of failed attempts
// without a Retry-After header, i.e. 5xx responses, connection errors, and
// any other failure the retry condition ([WithRetryCondition]) retries: the
// backoff starts at initial and doubles with each retry, up to maxWait, and
// each wait is a random duration between half and all of the backoff
// (jitter). It does not apply to responses with a Retry-After header, such as
// the API's 429 responses, which wait for the header instead (see
// [WithMaxRateLimitWait]). The default is 500ms initial and 3s max.
//
// initial must be 100ms–1 minute, and maxWait 100ms–5 minutes and at least
// initial; this is validated by [Client.Connect].
func WithRetryBackoff(initial, maxWait time.Duration) Option {
	return func(o *Options) {
		o.retryWaitTime = initial
		o.retryMaxWaitTime = maxWait
	}
}

// WithRetryWaitTime sets the initial backoff wait of [WithRetryBackoff].
// Values below 100ms are silently ignored.
//
// Deprecated: Use [WithRetryBackoff], which sets the initial and max wait together.
func WithRetryWaitTime(waitTime time.Duration) Option {
	return func(o *Options) {
		if waitTime >= minRetryWaitTime {
			o.retryWaitTime = waitTime
		}
	}
}

// WithRetryMaxWaitTime sets the max backoff wait of [WithRetryBackoff].
// Values below 100ms are silently ignored.
//
// Deprecated: Use [WithRetryBackoff], which sets the initial and max wait together.
func WithRetryMaxWaitTime(maxWaitTime time.Duration) Option {
	return func(o *Options) {
		if maxWaitTime >= minRetryMaxWaitTime {
			o.retryMaxWaitTime = maxWaitTime
		}
	}
}

// WithMaxRateLimitWait sets the longest Retry-After the client waits for
// before retrying a response with that header, such as the API's 429
// responses. The client waits the time in the header, plus a random extra of
// up to 20% so that concurrent requests do not all retry at the same moment.
//
// If the Retry-After is longer than this, the client returns the [*APIError]
// of the response right away, instead of a retry that could not succeed.
// [APIError.RetryAfter] tells when to try again. The same applies when the
// wait would pass the context deadline, which also stops backoff waits
// ([WithRetryBackoff]); then the last error is returned, which is a transport
// error when the last attempt got no response.
//
// The default is 1 minute. Valid range is 1s–10 minutes; values outside it
// are silently ignored. Retries of 429 responses count against
// [WithRetryCount].
func WithMaxRateLimitWait(maxWait time.Duration) Option {
	return func(o *Options) {
		if maxWait >= minMaxRateLimitWait && maxWait <= maxMaxRateLimitWait {
			o.maxRateLimitWait = maxWait
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

// WithRetryCondition sets the function that decides whether a failed attempt
// is retried, as long as retries remain ([WithRetryCount]). statusCode is the
// HTTP status code of the response, or 0 if no response was received (err is
// then non-nil). The default is [DefaultRetryCondition], which retries on 429,
// 5xx and transient connection errors; call it from a custom condition to
// extend the default behaviour.
//
// The condition only decides whether to retry. The wait is the Retry-After
// header when the response has one, such as the API's 429 responses
// ([WithMaxRateLimitWait]), and backoff otherwise ([WithRetryBackoff]).
//
// Nil values are silently ignored and the default is retained.
func WithRetryCondition(condition func(statusCode int, err error) bool) Option {
	return func(o *Options) {
		if condition != nil {
			o.retryPolicy = retryConditionPolicy(condition)
		}
	}
}

// WithRetryPolicy sets the retry condition as a function of the resty
// response. The response may be nil, or have no raw response, when err is
// non-nil. Nil values are silently ignored.
//
// Deprecated: Use [WithRetryCondition], which does not depend on resty types.
func WithRetryPolicy(policy func(*resty.Response, error) bool) Option {
	return func(o *Options) {
		if policy != nil {
			o.retryPolicy = policy
		}
	}
}

// retryConditionPolicy adapts a retry condition to the internal resty based retry policy.
func retryConditionPolicy(condition func(statusCode int, err error) bool) func(*resty.Response, error) bool {
	return func(r *resty.Response, err error) bool {
		return condition(statusCode(r), err)
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

// WithTimeout sets the timeout of a single HTTP request (one attempt). The
// default is 30 seconds. Valid range is 1 second–5 minutes. Values outside
// this range are silently ignored and the default is retained.
//
// An attempt that times out is retried by [DefaultRetryCondition]. The total
// time of a send, including retries and waits, is limited by the context
// deadline or [WithSendTimeout].
func WithTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		if timeout >= minTimeout && timeout <= maxTimeout {
			o.timeout = timeout
		}
	}
}

// WithSendTimeout sets the maximum total time of [Client.Send],
// [Client.SendWithResponse], [Client.Ping] and [Client.Connect], including
// retries, backoff and rate limit waits, when the context has no deadline. A
// context deadline always takes precedence. The default is 2 minutes. 0
// disables the limit, so that a call without a context deadline is only
// limited by the retry options.
//
// When a wait would pass the limit, the client returns the last error right
// away instead of waiting (e.g. an [*APIError] with status 429 and
// [APIError.RetryAfter] set).
//
// Valid values are 0 and 1 second–1 hour; others are silently ignored and the
// default is retained.
func WithSendTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		if timeout == 0 || (timeout >= minSendTimeout && timeout <= maxSendTimeout) {
			o.sendTimeout = timeout
		}
	}
}

// WithUserAgent sets the User-Agent header sent with every request. The
// default is "slack-manager-go-client/<module version>", e.g.
// "slack-manager-go-client/v0.5.0" ("dev" when the version is unknown). Empty
// values are silently ignored and the default is retained.
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
// to disable redirects entirely. A redirect beyond the maximum is not
// followed, and the redirect response is returned as an [*APIError] with its
// 3xx status code (not retried by [DefaultRetryCondition]). The default is
// 10. The maximum is 20. Negative values or values greater than 20 are
// silently ignored and the default is retained.
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
		return fmt.Errorf("retry backoff initial wait (WithRetryBackoff) must be at least %v", minRetryWaitTime)
	}

	if o.retryWaitTime > maxRetryWaitTime {
		return fmt.Errorf("retry backoff initial wait (WithRetryBackoff) must not exceed %v", maxRetryWaitTime)
	}

	if o.retryMaxWaitTime < minRetryMaxWaitTime {
		return fmt.Errorf("retry backoff max wait (WithRetryBackoff) must be at least %v", minRetryMaxWaitTime)
	}

	if o.retryMaxWaitTime > maxRetryMaxWaitTime {
		return fmt.Errorf("retry backoff max wait (WithRetryBackoff) must not exceed %v", maxRetryMaxWaitTime)
	}

	if o.retryMaxWaitTime < o.retryWaitTime {
		return fmt.Errorf("retry backoff max wait (%v) must be greater than or equal to the initial wait (%v) (WithRetryBackoff)", o.retryMaxWaitTime, o.retryWaitTime)
	}

	if o.maxRateLimitWait < minMaxRateLimitWait {
		return fmt.Errorf("max rate limit wait (WithMaxRateLimitWait) must be at least %v", minMaxRateLimitWait)
	}

	if o.maxRateLimitWait > maxMaxRateLimitWait {
		return fmt.Errorf("max rate limit wait (WithMaxRateLimitWait) must not exceed %v", maxMaxRateLimitWait)
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

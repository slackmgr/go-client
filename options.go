package client

import (
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
	}
}

// WithRetryCount sets the number of retry attempts for failed requests.
// Negative values are ignored. Maximum allowed is 100.
func WithRetryCount(count int) Option {
	return func(o *Options) {
		if count >= 0 {
			o.retryCount = count
		}
	}
}

// WithRetryWaitTime sets the initial wait time between retries.
// Values less than 100ms are ignored. Maximum allowed is 1 minute.
func WithRetryWaitTime(waitTime time.Duration) Option {
	return func(o *Options) {
		if waitTime >= 100*time.Millisecond {
			o.retryWaitTime = waitTime
		}
	}
}

// WithRetryMaxWaitTime sets the maximum wait time between retries.
// Values less than 100ms are ignored. Must be >= retryWaitTime. Maximum allowed is 5 minutes.
func WithRetryMaxWaitTime(maxWaitTime time.Duration) Option {
	return func(o *Options) {
		if maxWaitTime >= 100*time.Millisecond {
			o.retryMaxWaitTime = maxWaitTime
		}
	}
}

// WithRequestLogger sets the logger for HTTP request logging.
// Nil values are ignored.
func WithRequestLogger(logger RequestLogger) Option {
	return func(o *Options) {
		if logger != nil {
			o.requestLogger = logger
		}
	}
}

// WithRetryPolicy sets a custom retry policy function.
// Nil values are ignored.
func WithRetryPolicy(policy func(*resty.Response, error) bool) Option {
	return func(o *Options) {
		if policy != nil {
			o.retryPolicy = policy
		}
	}
}

// WithRequestHeader adds a custom header to all requests.
// Empty header names and attempts to override Content-Type or Accept are ignored.
func WithRequestHeader(header, value string) Option {
	return func(o *Options) {
		header = strings.TrimSpace(header)

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

	return nil
}

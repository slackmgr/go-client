package client

import (
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

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

func WithRetryCount(count int) Option {
	return func(conf *Options) {
		if count >= 0 {
			conf.retryCount = count
		}
	}
}

func WithRetryWaitTime(waitTime time.Duration) Option {
	return func(conf *Options) {
		if waitTime >= 100*time.Millisecond {
			conf.retryWaitTime = waitTime
		}
	}
}

func WithRetryMaxWaitTime(maxWaitTime time.Duration) Option {
	return func(conf *Options) {
		if maxWaitTime >= 100*time.Millisecond {
			conf.retryMaxWaitTime = maxWaitTime
		}
	}
}

func WithRequestLogger(logger RequestLogger) Option {
	return func(conf *Options) {
		if logger != nil {
			conf.requestLogger = logger
		}
	}
}

func WithRetryPolicy(policy func(*resty.Response, error) bool) Option {
	return func(conf *Options) {
		if policy != nil {
			conf.retryPolicy = policy
		}
	}
}

func WithRequestHeader(header, value string) Option {
	return func(conf *Options) {
		header = strings.TrimSpace(header)

		if header == "" || strings.EqualFold(header, "Content-Type") || strings.EqualFold(header, "Accept") {
			return
		}

		conf.requestHeaders[header] = value
	}
}

func WithBasicAuth(username, password string) Option {
	return func(conf *Options) {
		conf.basicAuthUsername = username
		conf.basicAuthPassword = password
	}
}

func WithAuthScheme(scheme string) Option {
	return func(conf *Options) {
		conf.authScheme = scheme
	}
}

func WithAuthToken(token string) Option {
	return func(conf *Options) {
		conf.authToken = token
	}
}

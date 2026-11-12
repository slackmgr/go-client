package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/slackmgr/types"
)

// Client is an HTTP client for sending alerts to the Slack Manager API.
// Use [New] to create a Client, then call [Client.Connect] to establish
// the connection. Call [Client.Close] when finished to release resources.
type Client struct {
	baseURL    string
	client     *resty.Client
	options    *Options
	once       sync.Once
	connectErr error
	transport  *http.Transport
	pacer      *pacer
}

type alertsList struct {
	Alerts []*types.Alert `json:"alerts"`
}

// apiErrorResponse represents the standard error response from the API.
type apiErrorResponse struct {
	Error string `json:"error"`
}

// ResponseMetadata contains metadata from the HTTP response returned by [Client.SendWithResponse].
type ResponseMetadata struct {
	Duration   time.Duration
	StatusCode int
	Headers    map[string]string
}

// New creates a new [Client] configured with the given base URL and options.
// Call [Client.Connect] before sending alerts.
func New(baseURL string, opts ...Option) *Client {
	options := newClientOptions()

	for _, o := range opts {
		o(options)
	}

	c := &Client{
		baseURL: baseURL,
		options: options,
	}

	if options.rateLimitPacing {
		c.pacer = newPacer(time.Now)
	}

	return c
}

// Connect initializes the HTTP client and validates connectivity by pinging
// the API. It is safe for concurrent use and only initializes once — if
// Connect fails, subsequent calls return the same error.
func (c *Client) Connect(ctx context.Context) error {
	c.once.Do(func() {
		if c.baseURL == "" {
			c.connectErr = errors.New("base URL must be set")
			return
		}

		if err := c.options.Validate(); err != nil {
			c.connectErr = fmt.Errorf("invalid options: %w", err)
			return
		}

		// Configure transport with connection pool settings
		c.transport = &http.Transport{
			MaxIdleConns:      c.options.maxIdleConns,
			MaxConnsPerHost:   c.options.maxConnsPerHost,
			IdleConnTimeout:   c.options.idleConnTimeout,
			DisableKeepAlives: c.options.disableKeepAlive,
			TLSClientConfig:   c.options.tlsConfig,
		}

		c.client = resty.New().
			SetBaseURL(c.baseURL).
			SetTimeout(c.options.timeout).
			SetTransport(c.transport).
			SetRedirectPolicy(resty.FlexibleRedirectPolicy(c.options.maxRedirects)).
			SetLogger(c.options.requestLogger).
			SetHeader("User-Agent", c.options.userAgent)

		for key, value := range c.options.requestHeaders {
			c.client.SetHeader(key, value)
		}

		if c.options.basicAuthUsername != "" {
			c.client.SetBasicAuth(c.options.basicAuthUsername, c.options.basicAuthPassword)
		} else if c.options.authToken != "" {
			c.client.SetAuthScheme(c.options.authScheme)
			c.client.SetAuthToken(c.options.authToken)
		}

		if err := c.ping(ctx); err != nil {
			c.connectErr = fmt.Errorf("failed to ping alerts API: %w", err)
			return
		}
	})

	return c.connectErr
}

// Send posts one or more alerts to the API. [Client.Connect] must be called
// first. Returns an error if the alerts slice is empty or any element is nil.
func (c *Client) Send(ctx context.Context, alerts ...*types.Alert) error {
	_, err := c.SendWithResponse(ctx, alerts...)
	return err
}

// SendWithResponse posts one or more alerts to the API and returns HTTP response metadata.
// [Client.Connect] must be called first. Returns an error if the alerts slice is empty or
// any element is nil. The returned *ResponseMetadata is non-nil whenever an HTTP response
// was received (even on non-2xx); it is nil when a network-level error prevents any
// response from arriving, or when client-side rate limit pacing ([WithRateLimitPacing])
// stopped the first attempt from being sent. After retries, it describes the last
// response received.
func (c *Client) SendWithResponse(ctx context.Context, alerts ...*types.Alert) (*ResponseMetadata, error) {
	if c == nil {
		return nil, errors.New("alert client is nil")
	}

	if c.client == nil {
		return nil, errors.New("client not connected - call Connect() first")
	}

	if len(alerts) == 0 {
		return nil, errors.New("alerts list cannot be empty")
	}

	for i, alert := range alerts {
		if alert == nil {
			return nil, fmt.Errorf("alert at index %d is nil", i)
		}
	}

	alertsInput := &alertsList{
		Alerts: c.withAlertClient(alerts),
	}

	var targets map[string]int
	if c.pacer != nil {
		targets = pacingTargets(alertsInput.Alerts)
	}

	body, err := json.Marshal(alertsInput)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal alerts list: %w", err)
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.options.sendTimeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, c.options.sendTimeout)
		defer cancel()
	}

	return c.postWithResponse(ctx, c.options.alertsEndpoint, body, targets)
}

// SendAlert posts a single alert to the API. It is equivalent to [Client.Send] with one alert.
//
// Deprecated: SendAlert exists for compatibility with the legacy client library. Use [Client.Send] instead.
func (c *Client) SendAlert(ctx context.Context, alert *types.Alert) error {
	return c.Send(ctx, alert)
}

// SendAlerts posts a list of alerts to the API. It is equivalent to calling [Client.Send] with the same alerts.
//
// Deprecated: SendAlerts exists for compatibility with the legacy client library. Use [Client.Send] instead.
func (c *Client) SendAlerts(ctx context.Context, alerts []*types.Alert) error {
	return c.Send(ctx, alerts...)
}

// Close releases idle connections held by the client. After Close is called
// the client should not be reused.
func (c *Client) Close() {
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}

// Ping checks connectivity to the API. [Client.Connect] must be called
// first. Use this to verify the connection is still healthy after the
// initial connect.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil {
		return errors.New("alert client is nil")
	}

	if c.client == nil {
		return errors.New("client not connected - call Connect() first")
	}

	return c.ping(ctx)
}

// RestyClient returns the underlying resty.Client for advanced configuration.
// Returns nil if [Client.Connect] has not been called. Use with caution:
// direct modifications may affect client behaviour. Retries are handled by
// [Client], not resty, so requests made directly on the resty client are not
// retried.
func (c *Client) RestyClient() *resty.Client {
	return c.client
}

func (c *Client) ping(ctx context.Context) error {
	return c.get(ctx, c.options.pingEndpoint)
}

func (c *Client) get(ctx context.Context, path string) error {
	response, attempts, err := c.execute(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", path, err)
	}

	if !response.IsSuccess() {
		return newAPIError(response, attempts)
	}

	return nil
}

func (c *Client) postWithResponse(ctx context.Context, path string, body []byte, targets map[string]int) (*ResponseMetadata, error) {
	response, attempts, err := c.execute(ctx, http.MethodPost, path, body, targets)

	if _, ok := errors.AsType[*APIError](err); ok {
		return responseMetadata(response), err
	}

	if err != nil {
		return nil, fmt.Errorf("POST %s failed: %w", path, err)
	}

	meta := responseMetadata(response)

	if !response.IsSuccess() {
		return meta, newAPIError(response, attempts)
	}

	return meta, nil
}

// responseMetadata returns the metadata of a response, or nil if there is none.
func responseMetadata(response *resty.Response) *ResponseMetadata {
	if response == nil || response.RawResponse == nil {
		return nil
	}

	return &ResponseMetadata{
		Duration:   response.Time(),
		StatusCode: response.StatusCode(),
		Headers:    flattenHeaders(response.Header()),
	}
}

// newAPIError creates an [*APIError] from a non-2xx response.
func newAPIError(response *resty.Response, attempts int) *APIError {
	wait, _ := retryAfter(response, time.Now())

	return &APIError{
		Method:     response.Request.Method,
		URL:        sanitizeURL(response.Request.URL),
		StatusCode: response.StatusCode(),
		Message:    getBodyErrorMessage(response),
		RetryAfter: wait,
		Attempts:   attempts,
	}
}

func flattenHeaders(h http.Header) map[string]string {
	headers := make(map[string]string, len(h))
	for key, values := range h {
		headers[key] = strings.Join(values, ", ")
	}

	return headers
}

func getBodyErrorMessage(response *resty.Response) string {
	body := response.Body()

	if len(body) == 0 {
		return "(empty error body)"
	}

	var apiErr apiErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != "" {
		return apiErr.Error
	}

	return string(body)
}

// sanitizeURL removes credentials (user info) from URLs to prevent leaking in logs.
func sanitizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	if parsed.User == nil {
		return rawURL
	}

	// Rebuild URL with redacted credentials to avoid URL encoding issues
	result := parsed.Scheme + "://***:***@" + parsed.Host + parsed.RequestURI()
	if parsed.Fragment != "" {
		result += "#" + parsed.Fragment
	}

	return result
}

// withAlertClient returns the alerts with the configured alert client (see [WithAlertClient]) set on those
// without a client. Alerts that need it are shallow copies, so the caller's alerts are never modified.
func (c *Client) withAlertClient(alerts []*types.Alert) []*types.Alert {
	if c.options.alertClient == "" {
		return alerts
	}

	result := make([]*types.Alert, len(alerts))

	for i, alert := range alerts {
		if strings.TrimSpace(alert.Client) != "" {
			result[i] = alert
			continue
		}

		cp := *alert
		cp.Client = c.options.alertClient
		result[i] = &cp
	}

	return result
}

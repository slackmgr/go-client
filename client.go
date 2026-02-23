package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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

	return &Client{
		baseURL: baseURL,
		options: options,
	}
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
			SetRetryCount(c.options.retryCount).
			SetRetryWaitTime(c.options.retryWaitTime).
			SetRetryMaxWaitTime(c.options.retryMaxWaitTime).
			AddRetryCondition(c.options.retryPolicy).
			SetRetryAfter(parseRetryAfterHeader).
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
// was received (even on non-2xx); it is nil only when a network-level error prevents any
// response from arriving.
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
		Alerts: alerts,
	}

	body, err := json.Marshal(alertsInput)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal alerts list: %w", err)
	}

	return c.postWithResponse(ctx, c.options.alertsEndpoint, body)
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
// direct modifications may affect client behaviour.
func (c *Client) RestyClient() *resty.Client {
	return c.client
}

func (c *Client) ping(ctx context.Context) error {
	return c.get(ctx, c.options.pingEndpoint)
}

func (c *Client) get(ctx context.Context, path string) error {
	request := c.client.R().SetContext(ctx)

	response, err := request.Get(path)
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", path, err)
	}

	if !response.IsSuccess() {
		return fmt.Errorf("GET %s failed with status code %d: %s", sanitizeURL(response.Request.URL), response.StatusCode(), getBodyErrorMessage(response))
	}

	return nil
}

func (c *Client) postWithResponse(ctx context.Context, path string, body []byte) (*ResponseMetadata, error) {
	request := c.client.R().SetContext(ctx).SetBody(body)

	response, err := request.Post(path)
	if err != nil {
		return nil, fmt.Errorf("POST %s failed: %w", path, err)
	}

	meta := &ResponseMetadata{
		Duration:   response.Time(),
		StatusCode: response.StatusCode(),
		Headers:    flattenHeaders(response.Header()),
	}

	if !response.IsSuccess() {
		return meta, fmt.Errorf("POST %s failed with status code %d: %s", sanitizeURL(response.Request.URL), response.StatusCode(), getBodyErrorMessage(response))
	}

	return meta, nil
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

// parseRetryAfterHeader extracts the Retry-After header value for rate limiting.
// Returns the duration to wait before retrying if the header is present.
func parseRetryAfterHeader(_ *resty.Client, resp *resty.Response) (time.Duration, error) {
	retryAfter := resp.Header().Get("Retry-After")
	if retryAfter == "" {
		return 0, nil
	}

	// Try parsing as seconds first
	if seconds, err := strconv.Atoi(retryAfter); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}

	// Try parsing as HTTP-date
	if t, err := http.ParseTime(retryAfter); err == nil {
		return time.Until(t), nil
	}

	return 0, nil
}

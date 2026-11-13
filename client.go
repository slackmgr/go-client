package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/slackmgr/types"
)

// Client is an HTTP client for sending alerts to the Slack Manager API.
// Use [New] to create a Client, then call [Client.Connect] to establish
// the connection. Call [Client.Close] when finished to release resources.
// All methods are safe for concurrent use.
type Client struct {
	baseURL   string
	options   *Options
	connectMu sync.Mutex                 // serializes Connect
	conn      atomic.Pointer[connection] // set when Connect succeeds
}

// Transport settings, as in [http.DefaultTransport].
const (
	dialTimeout           = 30 * time.Second
	dialKeepAlive         = 30 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	expectContinueTimeout = 1 * time.Second
)

// connection is the HTTP client of a connected [Client].
type connection struct {
	client    *resty.Client
	transport *http.Transport
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
// the API (with retries, see [WithRetryCount]). Like [Client.Send], it is
// limited to [WithSendTimeout] when ctx has no deadline. It is safe for
// concurrent use. Once Connect has succeeded, further calls return nil without
// doing anything. If it fails, the client stays unconnected and Connect can be
// called again, e.g. after the API has become available.
func (c *Client) Connect(ctx context.Context) error {
	if c == nil {
		return errors.New("alert client is nil")
	}

	c.connectMu.Lock()
	defer c.connectMu.Unlock()

	if c.conn.Load() != nil {
		return nil
	}

	if err := validateBaseURL(c.baseURL); err != nil {
		return err
	}

	if err := c.options.Validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	conn := c.newConnection()

	ctx, cancel := c.withSendTimeout(ctx)
	defer cancel()

	if err := c.ping(ctx, conn.client); err != nil {
		conn.transport.CloseIdleConnections()
		return fmt.Errorf("failed to ping alerts API: %w", err)
	}

	c.conn.Store(conn)

	return nil
}

// Send posts one or more alerts to the API. [Client.Connect] must be called
// first. Returns an error if the alerts slice is empty or any element is nil.
func (c *Client) Send(ctx context.Context, alerts ...*types.Alert) error {
	_, err := c.SendWithResponse(ctx, alerts...)
	return err
}

// SendWithResponse posts one or more alerts to the API and returns HTTP response metadata.
// [Client.Connect] must be called first. Returns an error if the alerts slice is empty or
// any element is nil.
//
// The returned *ResponseMetadata describes the response to the last attempt, even on
// non-2xx. It is nil when that attempt got no response (a network-level error). When the
// context is done while waiting for a retry, it describes the response to the previous
// attempt.
//
// When the context is done while waiting for a retry, the error wraps both the error of
// the previous attempt (e.g. an [*APIError]) and the context error.
func (c *Client) SendWithResponse(ctx context.Context, alerts ...*types.Alert) (*ResponseMetadata, error) {
	if c == nil {
		return nil, errors.New("alert client is nil")
	}

	conn := c.conn.Load()
	if conn == nil {
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

	body, err := json.Marshal(alertsInput)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal alerts list: %w", err)
	}

	ctx, cancel := c.withSendTimeout(ctx)
	defer cancel()

	return c.post(ctx, conn.client, c.options.alertsEndpoint, body)
}

// validateBaseURL checks that the base URL is an absolute http or https URL.
func validateBaseURL(baseURL string) error {
	if baseURL == "" {
		return errors.New("base URL must be set")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}

	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("base URL %s must be an absolute http or https URL", sanitizeURL(baseURL))
	}

	return nil
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
// the client should not be reused. Close on a nil or unconnected client does
// nothing.
func (c *Client) Close() {
	if c == nil {
		return
	}

	if conn := c.conn.Load(); conn != nil {
		conn.transport.CloseIdleConnections()
	}
}

// Ping checks connectivity to the API, with retries (see [WithRetryCount]).
// [Client.Connect] must be called first. Use this to verify the connection is
// still healthy after the initial connect. Like [Client.Send], it is limited
// to [WithSendTimeout] when ctx has no deadline.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil {
		return errors.New("alert client is nil")
	}

	conn := c.conn.Load()
	if conn == nil {
		return errors.New("client not connected - call Connect() first")
	}

	ctx, cancel := c.withSendTimeout(ctx)
	defer cancel()

	return c.ping(ctx, conn.client)
}

// RestyClient returns the underlying resty.Client for advanced configuration.
// Returns nil until [Client.Connect] has succeeded, and for a nil client. Use with caution:
// direct modifications may affect client behaviour. Retries are handled by
// [Client], not resty, so requests made directly on the resty client are not
// retried.
func (c *Client) RestyClient() *resty.Client {
	if c == nil {
		return nil
	}

	if conn := c.conn.Load(); conn != nil {
		return conn.client
	}

	return nil
}

// withSendTimeout returns ctx limited to [WithSendTimeout] if it has no deadline, and ctx itself otherwise.
func (c *Client) withSendTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline || c.options.sendTimeout <= 0 {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, c.options.sendTimeout)
}

// newConnection creates the HTTP client from the options.
func (c *Client) newConnection() *connection {
	// The settings of http.DefaultTransport (proxy from the environment, dial and TLS handshake timeouts, HTTP/2),
	// with the connection pool settings of the options.
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: dialKeepAlive,
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		MaxIdleConns:          c.options.maxIdleConns,
		MaxConnsPerHost:       c.options.maxConnsPerHost,
		IdleConnTimeout:       c.options.idleConnTimeout,
		DisableKeepAlives:     c.options.disableKeepAlive,
		// Cloned, as the transport adds HTTP/2 to the NextProtos of its TLS config.
		TLSClientConfig: c.options.tlsConfig.Clone(),
	}

	client := resty.New().
		SetBaseURL(c.baseURL).
		SetTimeout(c.options.timeout).
		SetTransport(transport).
		SetRedirectPolicy(redirectPolicy(c.options.maxRedirects)).
		SetLogger(c.options.requestLogger).
		SetHeader("User-Agent", c.options.userAgent)

	for key, value := range c.options.requestHeaders {
		client.SetHeader(key, value)
	}

	if c.options.basicAuthUsername != "" {
		client.SetBasicAuth(c.options.basicAuthUsername, c.options.basicAuthPassword)
	} else if c.options.authToken != "" {
		client.SetAuthScheme(c.options.authScheme)
		client.SetAuthToken(c.options.authToken)
	}

	return &connection{client: client, transport: transport}
}

// redirectPolicy follows up to maxRedirects redirects. Beyond that, the redirect response itself is returned
// (as an [*APIError]), instead of resty's "stopped after N redirects" error, which would be retried.
//
// via holds the requests made so far, so len(via) is the number of the redirect about to be followed. Following
// is delegated to resty's policy for its header handling, with a limit it never reaches: it stops when
// len(via) reaches its limit, i.e. it follows one redirect less than its limit.
func redirectPolicy(maxRedirects int) resty.RedirectPolicyFunc {
	follow := resty.FlexibleRedirectPolicy(maxRedirects + 1)

	return func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return http.ErrUseLastResponse
		}

		return follow.Apply(req, via)
	}
}

func (c *Client) ping(ctx context.Context, client *resty.Client) error {
	_, err := c.execute(ctx, client, http.MethodGet, c.options.pingEndpoint, nil)
	return err
}

func (c *Client) post(ctx context.Context, client *resty.Client, path string, body []byte) (*ResponseMetadata, error) {
	response, err := c.execute(ctx, client, http.MethodPost, path, body)
	return responseMetadata(response), err
}

// requestURL returns the URL of a request to path, with any credentials redacted.
func (c *Client) requestURL(path string) string {
	return sanitizeURL(strings.TrimSuffix(c.baseURL, "/") + "/" + strings.TrimPrefix(path, "/"))
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

// newAPIError creates an [*APIError] from a non-2xx response to a request with the given method and path.
func (c *Client) newAPIError(response *resty.Response, method, path string, attempts int) *APIError {
	wait, _ := retryAfter(response, time.Now())

	apiErr := &APIError{
		Method:     method,
		URL:        c.requestURL(path),
		StatusCode: statusCode(response),
		Message:    "(no response)",
		RetryAfter: wait,
		Attempts:   attempts,
	}

	if response != nil {
		apiErr.Message = getBodyErrorMessage(response)

		if response.Request != nil && response.Request.URL != "" {
			apiErr.URL = sanitizeURL(response.Request.URL)
		}
	}

	return apiErr
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

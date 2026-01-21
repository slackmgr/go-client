package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/go-resty/resty/v2"
	common "github.com/peteraglen/slack-manager-common"
)

// Client is an HTTP client for sending alerts to the Slack Manager API.
// Use New to create a Client, then call Connect to establish the connection.
type Client struct {
	baseURL string
	client  *resty.Client
	options *Options
	once    sync.Once
}

type alertsList struct {
	Alerts []*common.Alert `json:"alerts"`
}

// New creates a new Client with the given base URL and options.
// The client must be connected with Connect before sending alerts.
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

// Connect initializes the HTTP client and validates connectivity by pinging the API.
// This method is safe for concurrent use and will only initialize once.
func (c *Client) Connect(ctx context.Context) error {
	var connectErr error

	c.once.Do(func() {
		if c.baseURL == "" {
			connectErr = errors.New("base URL must be set")
			return
		}

		if err := c.options.Validate(); err != nil {
			connectErr = fmt.Errorf("invalid options: %w", err)
			return
		}

		c.client = resty.New().
			SetBaseURL(c.baseURL).
			SetRetryCount(c.options.retryCount).
			SetRetryWaitTime(c.options.retryWaitTime).
			SetRetryMaxWaitTime(c.options.retryMaxWaitTime).
			AddRetryCondition(c.options.retryPolicy).
			SetLogger(c.options.requestLogger)

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
			connectErr = fmt.Errorf("failed to ping alerts API: %w", err)
			return
		}
	})

	return connectErr
}

// Send posts one or more alerts to the API. Connect must be called first.
// Returns an error if any alert in the slice is nil.
func (c *Client) Send(ctx context.Context, alerts ...*common.Alert) error {
	if c == nil {
		return errors.New("alert client is nil")
	}

	if c.client == nil {
		return errors.New("client not connected - call Connect() first")
	}

	if len(alerts) == 0 {
		return errors.New("alerts list cannot be empty")
	}

	for i, alert := range alerts {
		if alert == nil {
			return fmt.Errorf("alert at index %d is nil", i)
		}
	}

	alertsInput := &alertsList{
		Alerts: alerts,
	}

	body, err := json.Marshal(alertsInput)
	if err != nil {
		return fmt.Errorf("failed to marshal alerts list: %w", err)
	}

	return c.post(ctx, "alerts", body)
}

func (c *Client) ping(ctx context.Context) error {
	return c.get(ctx, "ping")
}

func (c *Client) get(ctx context.Context, path string) error {
	request := c.client.R().SetContext(ctx)

	response, err := request.Get(path)
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", path, err)
	}

	if !response.IsSuccess() {
		return fmt.Errorf("GET %s failed with status code %d: %s", response.Request.URL, response.StatusCode(), getBodyErrorMessage(response))
	}

	return nil
}

func (c *Client) post(ctx context.Context, path string, body []byte) error {
	request := c.client.R().SetContext(ctx).SetBody(body)

	response, err := request.Post(path)
	if err != nil {
		return fmt.Errorf("POST %s failed: %w", path, err)
	}

	if !response.IsSuccess() {
		return fmt.Errorf("POST %s failed with status code %d: %s", response.Request.URL, response.StatusCode(), getBodyErrorMessage(response))
	}

	return nil
}

func getBodyErrorMessage(response *resty.Response) string {
	body := response.Body()

	if len(body) > 0 {
		return string(body)
	}

	return "(empty error body)"
}

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-resty/resty/v2"
	common "github.com/peteraglen/slack-manager-common"
)

type Client struct {
	baseURL string
	client  *resty.Client
	options *Options
}

type alertsList struct {
	Alerts []*common.Alert `json:"alerts"`
}

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

func (c *Client) Connect(ctx context.Context) (*Client, error) {
	if c.baseURL == "" {
		return nil, errors.New("base URL must be set")
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
		return nil, fmt.Errorf("failed to ping alerts API: %w", err)
	}

	return c, nil
}

func (c *Client) Send(ctx context.Context, alerts ...*common.Alert) error {
	if c == nil {
		return errors.New("alert client is nil")
	}

	if len(alerts) == 0 {
		return errors.New("alerts list cannot be empty")
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
		return fmt.Errorf("GET %s failed: %w", response.Request.URL, err)
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
		return fmt.Errorf("POST %s failed: %w", response.Request.URL, err)
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

package client

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"

	"github.com/go-resty/resty/v2"
)

func TestDefaultRetryPolicy_ContextCanceled(t *testing.T) {
	t.Parallel()

	result := DefaultRetryPolicy(nil, context.Canceled)

	if result {
		t.Error("expected false for context.Canceled")
	}
}

func TestDefaultRetryPolicy_ContextDeadlineExceeded(t *testing.T) {
	t.Parallel()

	result := DefaultRetryPolicy(nil, context.DeadlineExceeded)

	if result {
		t.Error("expected false for context.DeadlineExceeded")
	}
}

func TestDefaultRetryPolicy_DNSError(t *testing.T) {
	t.Parallel()

	dnsErr := &net.DNSError{
		Err:  "no such host",
		Name: "example.com",
	}

	result := DefaultRetryPolicy(nil, dnsErr)

	if result {
		t.Error("expected false for DNS error")
	}
}

func TestDefaultRetryPolicy_PermanentConnErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  syscall.Errno
	}{
		{"connection refused", syscall.ECONNREFUSED},
		{"network unreachable", syscall.ENETUNREACH},
		{"host unreachable", syscall.EHOSTUNREACH},
		{"permission denied", syscall.EACCES},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opErr := &net.OpError{Op: "dial", Net: "tcp", Err: tt.err}

			if DefaultRetryPolicy(nil, opErr) {
				t.Errorf("expected false for %s", tt.name)
			}
		})
	}
}

func TestDefaultRetryPolicy_OtherError(t *testing.T) {
	t.Parallel()

	result := DefaultRetryPolicy(nil, net.ErrClosed)

	if !result {
		t.Error("expected true for other connection errors")
	}
}

func TestDefaultRetryPolicy_Status429(t *testing.T) {
	t.Parallel()

	resp := createRestyResponse(t, 429)

	result := DefaultRetryPolicy(resp, nil)

	if !result {
		t.Error("expected true for status 429")
	}
}

func TestDefaultRetryPolicy_Status5xx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"500", 500},
		{"502", 502},
		{"503", 503},
		{"504", 504},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := createRestyResponse(t, tt.statusCode)

			result := DefaultRetryPolicy(resp, nil)

			if !result {
				t.Errorf("expected true for status %d", tt.statusCode)
			}
		})
	}
}

func TestDefaultRetryPolicy_Status2xx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"200", 200},
		{"201", 201},
		{"204", 204},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := createRestyResponse(t, tt.statusCode)

			result := DefaultRetryPolicy(resp, nil)

			if result {
				t.Errorf("expected false for status %d", tt.statusCode)
			}
		})
	}
}

func TestDefaultRetryPolicy_Status4xx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"400", 400},
		{"401", 401},
		{"403", 403},
		{"404", 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := createRestyResponse(t, tt.statusCode)

			result := DefaultRetryPolicy(resp, nil)

			if result {
				t.Errorf("expected false for status %d", tt.statusCode)
			}
		})
	}
}

// createRestyResponse creates a resty.Response with the given status code using httptest.
func createRestyResponse(t *testing.T, statusCode int) *resty.Response {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statusCode)
	}))
	// Close server after getting the response - don't use defer here
	// as the response body is already buffered by resty

	client := resty.New()
	resp, err := client.R().Get(server.URL)

	// Close the server after the request completes
	server.Close()

	if err != nil {
		t.Fatalf("failed to create response: %v", err)
	}

	return resp
}

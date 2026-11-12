package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

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

	// The client stops before calling the retry condition when the context of the send is done, so deadline
	// exceeded can only come from the request timeout of a single attempt, which is retried.
	if !DefaultRetryPolicy(nil, context.DeadlineExceeded) {
		t.Error("expected true for context.DeadlineExceeded")
	}
}

func TestDefaultRetryCondition_RequestTimeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	_, err := resty.New().SetTimeout(50 * time.Millisecond).R().Get(server.URL)
	if err == nil {
		t.Fatal("expected a timeout error")
	}

	if !DefaultRetryCondition(0, err) {
		t.Errorf("expected request timeout to be retried, got false for %v", err)
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

func TestDefaultRetryCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		err        error
		expected   bool
	}{
		{"429", http.StatusTooManyRequests, nil, true},
		{"500", http.StatusInternalServerError, nil, true},
		{"503", http.StatusServiceUnavailable, nil, true},
		{"200", http.StatusOK, nil, false},
		{"400", http.StatusBadRequest, nil, false},
		{"no response, transient error", 0, net.ErrClosed, true},
		{"no response, context canceled", 0, context.Canceled, false},
		{"no response, connection refused", 0, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, true},
		{"no response, host unreachable", 0, &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}, false},
		{"no response, no error", 0, nil, false},
		{"no response, certificate verification failed", 0, fmt.Errorf("wrapped: %w", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}), false},
		{"no response, server does not speak TLS", 0, fmt.Errorf("wrapped: %w", tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := DefaultRetryCondition(tt.statusCode, tt.err); got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestDefaultRetryPolicy_NilResponse(t *testing.T) {
	t.Parallel()

	if DefaultRetryPolicy(nil, nil) {
		t.Error("expected false for nil response without error")
	}
}

func TestClient_UntrustedCertificateNotRetried(t *testing.T) {
	t.Parallel()

	var count atomic.Int32

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// The test server's certificate is not trusted by the default TLS configuration.
	c := New(server.URL, WithRetryCount(3), WithRetryBackoff(100*time.Millisecond, 100*time.Millisecond))

	started := time.Now()
	err := c.Connect(t.Context())

	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); !ok {
		t.Fatalf("expected a certificate verification error, got %v", err)
	}

	if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
		t.Errorf("expected no retry, took %v", elapsed)
	}
}

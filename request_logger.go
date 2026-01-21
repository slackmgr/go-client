package client

// RequestLogger defines the interface for logging HTTP requests.
// Implement this interface to provide custom logging behavior.
type RequestLogger interface {
	Errorf(format string, v ...any)
	Warnf(format string, v ...any)
	Debugf(format string, v ...any)
}

// NoopLogger is a RequestLogger that discards all log messages.
// This is the default logger used when no custom logger is provided.
type NoopLogger struct{}

func (l *NoopLogger) Errorf(_ string, _ ...any) {}
func (l *NoopLogger) Warnf(_ string, _ ...any)  {}
func (l *NoopLogger) Debugf(_ string, _ ...any) {}

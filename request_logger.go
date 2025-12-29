package client

type RequestLogger interface {
	Errorf(format string, v ...any)
	Warnf(format string, v ...any)
	Debugf(format string, v ...any)
}

type NoopLogger struct{}

func (l *NoopLogger) Errorf(_ string, _ ...any) {}
func (l *NoopLogger) Warnf(_ string, _ ...any)  {}
func (l *NoopLogger) Debugf(_ string, _ ...any) {}

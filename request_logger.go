package client

type RequestLogger interface {
	Errorf(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Debugf(format string, v ...interface{})
}

type NoopLogger struct{}

func (l *NoopLogger) Errorf(_ string, _ ...interface{}) {}
func (l *NoopLogger) Warnf(_ string, _ ...interface{})  {}
func (l *NoopLogger) Debugf(_ string, _ ...interface{}) {}

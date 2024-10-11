package scraperapi

// noopLogger is a logger that does nothing
type noopLogger struct{}

func (l noopLogger) Errorf(_ string, _ ...any) {
	// no-op
}

func (l noopLogger) Warnf(_ string, _ ...any) {
	// no-op
}

func (l noopLogger) Debugf(_ string, _ ...any) {
	// no-op
}

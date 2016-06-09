package reign

import (
	"log"
	"testing"
)

type nullWriter struct{}

func (nw nullWriter) Write(p []byte) (n int, err error) {
	return
}

func TestWrapLoggerCoverage(t *testing.T) {
	t.Parallel()

	// mostly this tests that it doesn't crash...
	wl := WrapLogger(log.New(nullWriter{}, "", 0))
	wl.Trace("Hi!")
	wl.Info("Hi!")
	wl.Warn("Hi!")
	wl.Error("Hi!")
}

func TestStdLoggerCoverage(t *testing.T) {
	t.Parallel()

	stdLogger{}.Warn("Testing warn coverage")
}

func TestNullLoggerCoverage(t *testing.T) {
	t.Parallel()

	NullLogger.Warn("Test null warn coverage")
}

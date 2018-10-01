package reign

import (
	"log"
	"testing"
)

type nullWriter struct{}

func (nw nullWriter) Write(p []byte) (int, error) {
	return 0, nil
}

func TestWrapLoggerCoverage(t *testing.T) {
	t.Parallel()

	// mostly this tests that it doesn't crash...
	wl := WrapLogger(log.New(nullWriter{}, "", 0))

	wl.Error("Hi!")
	wl.Errorf("%s", "Hi!")
	wl.Warn("Hi!")
	wl.Warnf("%s", "Hi!")
	wl.Info("Hi!")
	wl.Infof("%s", "Hi!")
	wl.Trace("Hi!")
	wl.Tracef("%s", "Hi!")
}

func TestStdLoggerCoverage(t *testing.T) {
	t.Parallel()

	stdLogger{}.Error("Testing error coverage")
	stdLogger{}.Errorf("%s", "Testing errorf coverage")
	stdLogger{}.Warn("Testing warn coverage")
	stdLogger{}.Warnf("%s", "Testing warnf coverage")
	stdLogger{}.Info("Testing info coverage")
	stdLogger{}.Infof("%s", "Testing infof coverage")
	stdLogger{}.Trace("Testing trace coverage")
	stdLogger{}.Tracef("%s", "Testing tracef coverage")
}

func TestNullLoggerCoverage(t *testing.T) {
	t.Parallel()

	NullLogger.Error("Testing error coverage")
	NullLogger.Errorf("%s", "Testing errorf coverage")
	NullLogger.Warn("Testing warn coverage")
	NullLogger.Warnf("%s", "Testing warnf coverage")
	NullLogger.Info("Testing info coverage")
	NullLogger.Infof("%s", "Testing infof coverage")
	NullLogger.Trace("Testing trace coverage")
	NullLogger.Tracef("%s", "Testing tracef coverage")
}

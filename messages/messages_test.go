package messages

import (
	"fmt"
	"testing"
)

func TestLiota(t *testing.T) {
	expected := len(logLevels) + 1
	_ = liota("BLAH")

	if actual := len(logLevels); actual != expected {
		t.Errorf("liota() did not append to log levels slice: expected %d; actual %d", expected, actual)
	}
}

func TestLevel(t *testing.T) {
	t.Parallel()

	if actual := LevelTrace.String(); actual != "TRACE" {
		t.Errorf("Level.String() did not return expected result: actual = %s", actual)
	}

	badLevel := Level(99)

	if actual := badLevel.String(); actual != "UNKNOWN" {
		t.Errorf("Bad level did not return \"UNKNOWN\" as string representation: actual = %q", actual)
	}

	b, err := badLevel.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if actual := string(b); actual != "\"UNKNOWN\"" {
		t.Errorf("Bad level did not return \"UNKNOWN\" upon marshalling: actual = %s", actual)
	}
}

func TestStringer(t *testing.T) {
	t.Parallel()

	s1 := interface{}(stringerify("test"))

	if s2, ok := s1.(fmt.Stringer); !ok {
		t.Fatal("Stringified string doesn't implement the fmt.Stringer interface")
	} else if actual := s2.String(); actual != "test" {
		t.Errorf("expected \"test\"; actual = %q", actual)
	}
}

func TestLogMessages(t *testing.T) {
	t.Parallel()

	if trace := Trace("trace"); trace.Level != LevelTrace {
		t.Error("Trace() does not return a LogMessage of level TRACE")
	}

	if info := Info("info"); info.Level != LevelInfo {
		t.Error("Info() does not return a LogMessage of level INFO")
	}

	if warn := Warn("warn"); warn.Level != LevelWarn {
		t.Error("Warn() does not return a LogMessage of level WARN")
	}

	if err := Error("info"); err.Level != LevelError {
		t.Error("Error() does not return a LogMessage of level ERROR")
	}
}

func TestLogMessage(t *testing.T) {
	t.Parallel()

	trace := Trace("test")

	if actual := trace.String(); actual != "[TRACE] reign: test" {
		t.Errorf("Unexpected string result from default Trace(): %q", actual)
	}

	// Make sure an empty Name field doesn't show up in the string.
	trace.Name = ""

	if actual := trace.String(); actual != "[TRACE] test" {
		t.Errorf("Unexpected string result from Trace() with empty Name: %q", actual)
	}

	// Make sure we can create a LogMessage from a fmt.Stringer.
	trace = Trace(stringerify("test"))
	if actual := trace.String(); actual != "[TRACE] reign: test" {
		t.Errorf("Unexpected string result from default Trace(fmt.Stringer): %q", actual)
	}

	// Make sure we can create a LogMessage from something other than a string or fmt.Stringer.
	trace = Trace(1234567890)
	if actual := trace.String(); actual != "[TRACE] reign: 1234567890" {
		t.Errorf("Unexpected string result from default Trace(1234567890): %q", actual)
	}
}

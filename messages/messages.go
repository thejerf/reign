package messages

/*
The intention of the messages package is to provide message objects that are
compatible with string and structured loggers that may be passed into Reign.
*/

import (
	"encoding/json"
	"fmt"
)

const name = "reign"

// Level is the base type for log message levels.
type Level int

// String returns the Level's string presentation.
func (l Level) String() string {
	if int(l) < len(logLevels) {
		return logLevels[l]
	}

	return "UNKNOWN"
}

// MarshalJSON returns a byte slice of the Level's string representation.
func (l Level) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", l)), nil
}

// LogMessage is used to create an appropriate log message no matter if used in
// structured logging or string logging.
type LogMessage struct {
	// Name is the message's namespace, defaulted to "reign."
	Name string `json:"name,omitempty"`
	// Level is the log message's log level.
	Level Level `json:"level"`
	// Message is the message's payload, anything that implements the fmt.Stringer interface.
	Message fmt.Stringer `json:"message"`
}

func (lm *LogMessage) String() string {
	var n string

	if lm.Name != "" {
		n = lm.Name + ": "
	}

	return fmt.Sprintf("[%s] %s%s", lm.Level, n, lm.Message)
}

var (
	LevelTrace = liota("TRACE")
	LevelInfo  = liota("INFO")
	LevelWarn  = liota("WARN")
	LevelError = liota("ERROR")

	logLevels []string

	_ fmt.Stringer   = (*Level)(nil)
	_ json.Marshaler = (*Level)(nil)
	_ fmt.Stringer   = (*LogMessage)(nil)
)

// liota is the Level equivalent of iota.
func liota(s string) Level {
	logLevels = append(logLevels, s)
	return Level(len(logLevels) - 1)
}

// stringerify can be used to make a string implement fmt.Stringer.
type stringerify string

func (s stringerify) String() string {
	return string(s)
}

// message accepts a log Level and a message, and returns a pointer to
// a LogMessage object.  The LogMessage object implements the fmt.Stringer
// interface for use with regular loggers, and can be marshalled to JSON
// for use with structured loggers.
func message(l Level, m interface{}) *LogMessage {
	lm := &LogMessage{Name: name, Level: l}

	switch msg := m.(type) {
	case string:
		lm.Message = stringerify(msg)
	case fmt.Stringer:
		lm.Message = msg
	default:
		lm.Message = stringerify(fmt.Sprintf("%v", msg))
	}

	return lm
}

// Trace returns a LogMessage of severity TRACE.
func Trace(m interface{}) *LogMessage {
	return message(LevelTrace, m)
}

// Info returns a LogMessage of severity INFO.
func Info(m interface{}) *LogMessage {
	return message(LevelInfo, m)
}

// Warn returns a LogMessage of severity WARN.
func Warn(m interface{}) *LogMessage {
	return message(LevelWarn, m)
}

// Error returns a LogMessage of severity ERROR.
func Error(m interface{}) *LogMessage {
	return message(LevelError, m)
}

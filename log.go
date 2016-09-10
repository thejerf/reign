package reign

import (
	"fmt"
	"log"
)

// A ClusterLogger is the logging interface used by the Cluster system.
//
// The clustering system uses Info for situations that are not problems.
// This includes:
//  * Address resolution progress of remote cluster nodes. (Common DNS
//    problems or misconfigurations can cause excessive times for
//    resolution. This should give enough visibility into the resolution
//    process to rapidly identify the problem.)
//
// The clustering system uses Warn for situations that are problematic
// and you need to know about them, but are generally "expected" and may
// resolve themselves without any direction action. (That is, in general,
// losing network connections is "bad", but also perfectly normal and
// expected.) The clustering system uses Warn for:
//  * Connections established and lost to the other nodes
//  * Attempts to update the cluster configuration that fail due to
//    invalid configuration
//
// The clustering system uses Error for situations that prevent connection
// to some target node, and will most likely not resolve themselves without
// active human intervention. The clustering system will user Error for:
//  * Handshake with foreign node failed due to:
//    * Remote said they had a different NodeID than I expected.
//    * Incompatible clustering version.
//    * Failed SSL handshake.
// The goal is that all Errors are things that should fire alarming
// systems, and all things that should fire alarming systems are Errors.
//
// You can wrap a standard *log.Logger with the provided WrapLogger.
type ClusterLogger interface {
	Error(string)
	Errorf(format string, args ...interface{})
	Warn(string)
	Warnf(format string, args ...interface{})
	Info(string)
	Infof(format string, args ...interface{})
	Trace(string)
	Tracef(format string, args ...interface{})
}

// WrapLogger takes as standard *log.Logger and returns a ClusterLogger
// that uses that logger.
func WrapLogger(l *log.Logger) ClusterLogger {
	return wrapLogger{l}
}

type wrapLogger struct {
	logger *log.Logger
}

func (sl wrapLogger) Error(s string) {
	sl.Errorf("%s", s)
}

func (sl wrapLogger) Errorf(format string, args ...interface{}) {
	sl.logger.Output(2, fmt.Sprintf("[ERROR] reign: "+format+"\n", args...))
}

func (sl wrapLogger) Warn(s string) {
	sl.Warnf("%s", s)
}

func (sl wrapLogger) Warnf(format string, args ...interface{}) {
	sl.logger.Output(2, fmt.Sprintf("[WARN] reign: "+format+"\n", args...))
}

func (sl wrapLogger) Info(s string) {
	sl.Infof("%s", s)
}

func (sl wrapLogger) Infof(format string, args ...interface{}) {
	sl.logger.Output(2, fmt.Sprintf("[INFO] reign: "+format+"\n", args...))
}

func (sl wrapLogger) Trace(s string) {
	sl.Tracef("%s", s)
}

func (sl wrapLogger) Tracef(format string, args ...interface{}) {
	sl.logger.Output(2, fmt.Sprintf("[TRACE] reign: "+format+"\n", args...))
}

// StdLogger is a ClusterLogger that will use the log.Output function
// from the standard logging package.
var StdLogger = stdLogger{}

type stdLogger struct{}

func (sl stdLogger) Error(s string) {
	sl.Errorf("%s", s)
}

func (sl stdLogger) Errorf(format string, args ...interface{}) {
	fmt.Printf("[ERROR] reign: "+format+"\n", args...)
}

func (sl stdLogger) Warn(s string) {
	sl.Warnf("%s", s)
}

func (sl stdLogger) Warnf(format string, args ...interface{}) {
	fmt.Printf("[WARN] reign: "+format+"\n", args...)
}

func (sl stdLogger) Info(s string) {
	sl.Infof("%s", s)
}

func (sl stdLogger) Infof(format string, args ...interface{}) {
	fmt.Printf("[INFO] reign: "+format+"\n", args...)
}

func (sl stdLogger) Trace(s string) {
	sl.Tracef("%s", s)
}

func (sl stdLogger) Tracef(format string, args ...interface{}) {
	fmt.Printf("[TRACE] reign: "+format+"\n", args...)
}

// NullLogger implements ClusterLogger, and throws all logging messages away.
var NullLogger = nullLogger{}

type nullLogger struct{}

func (nl nullLogger) Error(s string)                            {}
func (nl nullLogger) Errorf(format string, args ...interface{}) {}
func (nl nullLogger) Warn(s string)                             {}
func (nl nullLogger) Warnf(format string, args ...interface{})  {}
func (nl nullLogger) Info(s string)                             {}
func (nl nullLogger) Infof(format string, args ...interface{})  {}
func (nl nullLogger) Trace(s string)                            {}
func (nl nullLogger) Tracef(format string, args ...interface{}) {}

var (
	_ ClusterLogger = (*wrapLogger)(nil)
	_ ClusterLogger = (*stdLogger)(nil)
	_ ClusterLogger = (*nullLogger)(nil)
)

package reign

import (
	"fmt"
	"log"
	"testing"
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
	Error(...interface{})
	Errorf(format string, args ...interface{})
	Warn(...interface{})
	Warnf(format string, args ...interface{})
	Info(...interface{})
	Infof(format string, args ...interface{})
	Trace(...interface{})
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

func (sl wrapLogger) Error(args ...interface{}) {
	_ = sl.logger.Output(2, "[ERROR] reign: "+fmt.Sprint(args...)+"\n")
}

func (sl wrapLogger) Errorf(format string, args ...interface{}) {
	_ = sl.logger.Output(2, fmt.Sprintf("[ERROR] reign: "+format+"\n", args...))
}

func (sl wrapLogger) Warn(args ...interface{}) {
	_ = sl.logger.Output(2, "[WARN] reign: "+fmt.Sprint(args...)+"\n")
}

func (sl wrapLogger) Warnf(format string, args ...interface{}) {
	_ = sl.logger.Output(2, fmt.Sprintf("[WARN] reign: "+format+"\n", args...))
}

func (sl wrapLogger) Info(args ...interface{}) {
	_ = sl.logger.Output(2, "[INFO] reign: "+fmt.Sprint(args...)+"\n")
}

func (sl wrapLogger) Infof(format string, args ...interface{}) {
	_ = sl.logger.Output(2, fmt.Sprintf("[INFO] reign: "+format+"\n", args...))
}

func (sl wrapLogger) Trace(args ...interface{}) {
	_ = sl.logger.Output(2, "[TRACE] reign: "+fmt.Sprint(args...)+"\n")
}

func (sl wrapLogger) Tracef(format string, args ...interface{}) {
	_ = sl.logger.Output(2, fmt.Sprintf("[TRACE] reign: "+format+"\n", args...))
}

// StdLogger is a ClusterLogger that will use the log.Output function
// from the standard logging package.
var StdLogger = stdLogger{}

type stdLogger struct{}

func (sl stdLogger) Error(args ...interface{}) {
	fmt.Println("[ERROR] reign: " + fmt.Sprint(args...))
}

func (sl stdLogger) Errorf(format string, args ...interface{}) {
	fmt.Printf("[ERROR] reign: "+format+"\n", args...)
}

func (sl stdLogger) Warn(args ...interface{}) {
	fmt.Println("[WARN] reign: " + fmt.Sprint(args...))
}

func (sl stdLogger) Warnf(format string, args ...interface{}) {
	fmt.Printf("[WARN] reign: "+format+"\n", args...)
}

func (sl stdLogger) Info(args ...interface{}) {
	fmt.Println("[INFO] reign: " + fmt.Sprint(args...))
}

func (sl stdLogger) Infof(format string, args ...interface{}) {
	fmt.Printf("[INFO] reign: "+format+"\n", args...)
}

func (sl stdLogger) Trace(args ...interface{}) {
	fmt.Println("[TRACE] reign: " + fmt.Sprint(args...))
}

func (sl stdLogger) Tracef(format string, args ...interface{}) {
	fmt.Printf("[TRACE] reign: "+format+"\n", args...)
}

type testLogger struct {
	t *testing.T
}

func (tl testLogger) Error(args ...interface{}) {
	tl.t.Log("[ERROR] reign: " + fmt.Sprint(args...))
}

func (tl testLogger) Errorf(format string, args ...interface{}) {
	tl.t.Logf("[ERROR] reign: "+format+"\n", args...)
}

func (tl testLogger) Warn(args ...interface{}) {
	tl.t.Log("[WARN] reign: " + fmt.Sprint(args...))
}

func (tl testLogger) Warnf(format string, args ...interface{}) {
	tl.t.Logf("[WARN] reign: "+format+"\n", args...)
}

func (tl testLogger) Info(args ...interface{}) {
	tl.t.Log("[INFO] reign: " + fmt.Sprint(args...))
}

func (tl testLogger) Infof(format string, args ...interface{}) {
	tl.t.Logf("[INFO] reign: "+format+"\n", args...)
}

func (tl testLogger) Trace(args ...interface{}) {
	tl.t.Log("[TRACE] reign: " + fmt.Sprint(args...))
}

func (tl testLogger) Tracef(format string, args ...interface{}) {
	tl.t.Logf("[TRACE] reign: "+format+"\n", args...)
}

// NullLogger implements ClusterLogger, and throws all logging messages away.
var NullLogger = nullLogger{}

type nullLogger struct{}

func (nl nullLogger) Error(args ...interface{})                 {}
func (nl nullLogger) Errorf(format string, args ...interface{}) {}
func (nl nullLogger) Warn(args ...interface{})                  {}
func (nl nullLogger) Warnf(format string, args ...interface{})  {}
func (nl nullLogger) Info(args ...interface{})                  {}
func (nl nullLogger) Infof(format string, args ...interface{})  {}
func (nl nullLogger) Trace(args ...interface{})                 {}
func (nl nullLogger) Tracef(format string, args ...interface{}) {}

var (
	_ ClusterLogger = (*nullLogger)(nil)
	_ ClusterLogger = (*stdLogger)(nil)
	_ ClusterLogger = (*testLogger)(nil)
	_ ClusterLogger = (*wrapLogger)(nil)
)

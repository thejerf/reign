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
	// Used in debugging, should ship commented out.
	Trace(interface{}, ...interface{})

	Info(interface{}, ...interface{})
	Warn(interface{}, ...interface{})
	Error(interface{}, ...interface{})
}

// WrapLogger takes as standard *log.Logger and returns a ClusterLogger
// that uses that logger.
func WrapLogger(l *log.Logger) ClusterLogger {
	return wrapLogger{l}
}

type wrapLogger struct {
	logger *log.Logger
}

func (sl wrapLogger) Trace(s interface{}, vals ...interface{}) {
	sl.logger.Output(2, fmt.Sprintf("[TRAC] reign: "+fmt.Sprintf("%v", s), vals...))
}

func (sl wrapLogger) Info(s interface{}, vals ...interface{}) {
	sl.logger.Output(2, fmt.Sprintf("[INFO] reign: "+fmt.Sprintf("%v", s), vals...))
}

func (sl wrapLogger) Warn(s interface{}, vals ...interface{}) {
	sl.logger.Output(2, fmt.Sprintf("[WARN] reign: "+fmt.Sprintf("%v", s), vals...))
}

func (sl wrapLogger) Error(s interface{}, vals ...interface{}) {
	sl.logger.Output(2, fmt.Sprintf("[ERR] reign: "+fmt.Sprintf("%v", s), vals...))
}

// StdLogger is a ClusterLogger that will use the log.Output function
// from the standard logging package.
var StdLogger = stdLogger{}

type stdLogger struct{}

func (sl stdLogger) Trace(s interface{}, vals ...interface{}) {
	log.Printf("[TRAC] reign: "+fmt.Sprintf("%v", s), vals...)
}
func (sl stdLogger) Info(s interface{}, vals ...interface{}) {
	log.Printf("[INFO] reign: "+fmt.Sprintf("%v", s), vals...)
}
func (sl stdLogger) Warn(s interface{}, vals ...interface{}) {
	log.Printf("[WARN] reign: "+fmt.Sprintf("%v", s), vals...)
}
func (sl stdLogger) Error(s interface{}, vals ...interface{}) {
	log.Printf("[ERR] reign: "+fmt.Sprintf("%v", s), vals...)
}

// NullLogger implements ClusterLogger, and throws all logging messages away.
var NullLogger = nullLogger{}

type nullLogger struct{}

func (nl nullLogger) Trace(s interface{}, vals ...interface{}) {}
func (nl nullLogger) Info(s interface{}, vals ...interface{})  {}
func (nl nullLogger) Warn(s interface{}, vals ...interface{})  {}
func (nl nullLogger) Error(s interface{}, vals ...interface{}) {}

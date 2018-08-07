package reign

import (
	"encoding/gob"
	"net"
	"time"

	"github.com/thejerf/reign/internal"
)

var (
	// DefaultPingInterval determines the minimum interval between PING messages.
	DefaultPingInterval = time.Second * 30

	// MaxSequentialPingFailures is the maximum number of sequential ping failures
	// tolerable before the pinger panics, triggering a service restart if running
	// under suture.
	MaxSequentialPingFailures uint8 = 5
)

// pingRemote sends a "ping" message to the output encoder on an interval read off the
// resetTimer channel.  This interval is used until one greater than 0 is put on the
// channel.
//
// An initial interval can be put on the resetTimer channel before calling this function.
// Otherwise, the DefaultPingInterval is used.
//
// This function is meant to run as a goroutine.  The function will exit when the
// resetTimer channel is closed.
func pingRemote(output *gob.Encoder, resetTimer <-chan time.Duration, log ClusterLogger) {
	var (
		failureCount uint8
		interval                             = DefaultPingInterval
		ping         internal.ClusterMessage = internal.Ping{}
	)

	// Set the interval if there's already one on the channel.  Otherwise,
	// use the default.
	select {
	case i := <-resetTimer:
		if i > 0 {
			interval = i
		}
	default:
	}

	t := time.NewTimer(interval)

	for {
		select {
		case i, ok := <-resetTimer:
			if !ok {
				return
			}

			if i > 0 {
				interval = i
			}

			// Stop the current timer and drain its channel if necessary.
			if !t.Stop() {
				<-t.C
			}
			_ = t.Reset(interval)
		case <-t.C:
			err := output.Encode(&ping)
			if err != nil {
				failureCount++

				netErr, ok := err.(net.Error)
				if !ok || !netErr.Temporary() || failureCount >= MaxSequentialPingFailures {
					panic(err)
				}

				log.Error(err)
			} else {
				failureCount = 0
			}
			_ = t.Reset(interval)
		}
	}
}

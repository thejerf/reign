package reign

import (
	"encoding/gob"
	"time"

	"github.com/thejerf/reign/internal"
)

// DefaultPingInterval determines the minimum interval between PING messages.
var DefaultPingInterval = time.Second * 30

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
		interval                         = DefaultPingInterval
		ping     internal.ClusterMessage = internal.Ping{}
	)

	// Set the interval if there's already one on the channel.  Otherwise,
	// use the default.
	select {
	case i := <-resetTimer:
		if i.Nanoseconds() > 0 {
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

			if i.Nanoseconds() > 0 {
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
				log.Error(err)
			}
			_ = t.Reset(interval)
		}
	}
}

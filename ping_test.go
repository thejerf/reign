package reign

import (
	"bytes"
	"encoding/gob"
	"net"
	"testing"
	"time"
)

type tempNet struct {
	bytes.Buffer
}

func (t *tempNet) Write(p []byte) (int, error) {
	return 0, &net.DNSError{IsTemporary: true}
}

func TestTemporaryErrorThreshold(t *testing.T) {
	DefaultPingInterval = time.Second
	MaxSequentialPingFailures = 2
	success := make(chan struct{})
	expire := time.NewTimer(time.Second * 5)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				close(success)
			}
		}()
		pingRemote(
			gob.NewEncoder(new(tempNet)),
			make(chan time.Duration),
			NullLogger,
		)
	}()

	select {
	case <-success:
	case <-expire.C:
		t.Error("Threshold exceeded without a panic")
	}
}

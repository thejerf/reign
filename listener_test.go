package reign

import (
	"net"
	"sync"
	"testing"
)

func TestCoverNilListener(t *testing.T) {
	t.Parallel()

	var nl *nodeListener
	nl.waitForListen()
}

func TestCoverIncomingConnection(t *testing.T) {
	t.Parallel()

	var ic *incomingConnection
	err := ic.send(nil)
	if err == nil {
		t.Fatal("incoming connection can send to nowhere")
	}
}

type S struct{}

func (s S) String() string {
	return "string"
}

func TestMyStringCover(t *testing.T) {
	t.Parallel()

	myString(S{})
}

func TestNodeListenerErrors(t *testing.T) {
	ntb := testbed(nil)
	defer ntb.terminate()

	nl := &nodeListener{
		node:             ntb.c1.ThisNode,
		connectionServer: ntb.c1,
		ClusterLogger:    NullLogger,
		remoteMailboxes:  make(map[NodeID]*remoteMailboxes),
	}
	nl.condition = sync.NewCond(&nl.Mutex)

	if !panics(func() { nl.mailboxesForNode(10) }) {
		t.Fatal("Can get mailboxes that don't exist.")
	}

	// verify that it doesn't really serve, but terminates as expected.
	nl.stopped = true
	nl.Serve()

	nl.stopped = false
	// this is an IP address that hopefully you don't actually have
	// permission to bind to.
	badTCPAddr, err := net.ResolveTCPAddr("tcp4", "253.255.255.254:8")
	if err != nil {
		panic(err)
	}

	nl.node.listenaddr = nil
	if !panics(func() { nl.Serve() }) {
		t.Fatal("Node is happy to start serving with no listen.")
	}

	nl.node.listenaddr = badTCPAddr
	if !panics(func() { nl.Serve() }) {
		t.Fatal("We can listen on an invalid TCP addr?")
	}

	goodTCPAddr, err := net.ResolveTCPAddr("tcp4", "127.0.0.1:29876")
	if err != nil {
		panic(err)
	}
	nl.node.listenaddr = goodTCPAddr
	terminated := make(chan struct{})
	go func() {
		nl.Serve()
		terminated <- struct{}{}
	}()
	nl.waitForListen()
	nl.listener.Close()
	<-terminated
}

func TestListenerSSLHandshakeFailures(t *testing.T) {
	ntb := unstartedTestbed(nil)
	// we never start the servers, so we only need this
	defer ntb.terminateMailboxes()

	ntb.c2.listener.failOnSSLHandshake = true
	thingsTerminateOnFailure(t, ntb)
}

func TestListenerClusterHandshakeFailures(t *testing.T) {
	ntb := unstartedTestbed(nil)
	defer ntb.terminateMailboxes()

	ntb.c2.listener.failOnClusterHandshake = true
	thingsTerminateOnFailure(t, ntb)
}

func TestNodeSSLHandshakeFailures(t *testing.T) {
	ntb := unstartedTestbed(nil)
	defer ntb.terminateMailboxes()

	ntb.c1.nodeConnectors[2].failOnSSLHandshake = true
	thingsTerminateOnFailure(t, ntb)
}

func TestNodeClusterHandshakeFailure(t *testing.T) {
	ntb := unstartedTestbed(nil)
	defer ntb.terminateMailboxes()

	ntb.c1.nodeConnectors[2].failOnClusterHandshake = true
	thingsTerminateOnFailure(t, ntb)
}

func thingsTerminateOnFailure(t *testing.T, ntb *NetworkTestBed) {
	// this reaches in to serve the listener socket directly
	done := make(chan struct{})
	go func() {
		defer func() {
			done <- struct{}{}
		}()
		ntb.c2.listener.Serve()
	}()
	ntb.c2.waitForListen()

	// and this reaches in to directly run the "connect to node 2" function
	go func() {
		defer func() {
			done <- struct{}{}
		}()
		ntb.c1.nodeConnectors[2].Serve()
	}()

	// Assert that in the case of failure to connect due to SSL negotiation
	// failures, both systems terminate, so suture can pick them up.
	<-done
	ntb.c2.listener.Stop()
	<-done
}

func TestCoverStopNodeListener(t *testing.T) {
	t.Parallel()

	nl := new(nodeListener)
	nl.Stop()
	if !nl.stopped {
		t.Fatal("Could not stop the node listener.")
	}
}

func BenchmarkMinimalMessageSend(b *testing.B) {
	ntb := testbed(nil)
	defer ntb.terminate()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ntb.rem1_1.Send("a")
		ntb.mailbox1_1.ReceiveNext()
	}
}

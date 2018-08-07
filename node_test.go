package reign

// In this file, we take advantage of the provisions we've made for testing
// within the rest of the code base. Thus, mailboxes will belong to one
// cluster or another. By convention, we name the test mailboxes as so:
// mailbox#A_#B
// where #A is an incrementing counter keeping track of the mailbox itself,
// and #B is the node number that the mailbox corresponds to.
// adding From#C means that the mailboxs is from the point of view
// of the given node, so, mailbox1_1From2 means "a boundRemoteAddress
// that will send to mailbox1_1 from the point of view of node 2"
// Since we use the connection server a lot, c# is the connection server
// for the given node number.

// TODO:
// * Test lost connection restored.
// * Test linking works normally
// * Test linking works when connection terminated.
import (
	"testing"
	"time"

	"github.com/thejerf/reign/internal"
)

func init() {
	// DefaultPingInterval should be set to a small value so it doesn't unduly hang up heartbeat
	// tests, yet it should be large enough so as to give reign enough time to transfer
	// the ping message and its corresponding pong message over the network before the ping
	// timer expires.  Since these tests (or at least the HeartbeatRoundtrip test) are using
	// nodes whose messages are traversing localhost, 1 second should be sufficient.
	DefaultPingInterval = time.Second * 2
}

// this goes ahead and just lets the nodes talk over the network

// This function grabs the test bed and runs basic tests on it to
// establish that it is fundamentally working.
func TestMinimalTestBed(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	if ntb.node1address1.mailboxID == ntb.node2address1.mailboxID {
		t.Fatal("Both mailboxes have the same ID")
	}

	// Send "hello" from node 2 for address1 on node 1, destined
	// for mailbox1 on node 1.
	m := "hello"
	if err := ntb.fromNode2toNode1mailbox1.Send(m); err != nil {
		t.Fatal(err)
	}

	// Receive the message in mailbox1 on node 1.
	msg, ok := ntb.node1mailbox1.ReceiveNextTimeout(timeout)
	if !ok {
		t.Fatalf("Did not receive %q message", m)
	}
	if msg.(string) != m {
		t.Fatal("Could not send message between nodes for some reason")
	}

	m = "world"
	if err := ntb.fromNode2toNode1mailbox2.Send(m); err != nil {
		t.Fatal(err)
	}
	msg, ok = ntb.node2mailbox1.ReceiveNextTimeout(timeout)
	if !ok {
		t.Fatalf("Did not receive %q message", m)
	}
	if msg.(string) != m {
		t.Fatalf("Did not receive the expected %q response", m)
	}

	m = "Checking 1_2"
	if err := ntb.fromNode2toNode1mailbox2.Send(m); err != nil {
		t.Fatal(err)
	}
	msg, ok = ntb.node2mailbox1.ReceiveNextTimeout(timeout)
	if !ok {
		t.Fatalf("Did not receive %q message", m)
	}
	if msg.(string) != m {
		t.Fatal("Mailbox 1_2 is broken remotely.")
	}

	m = "Checking 2_2"
	if err := ntb.fromNode1toNode2mailbox2.Send(m); err != nil {
		t.Fatal(err)
	}
	msg, ok = ntb.node2mailbox2.ReceiveNextTimeout(timeout)
	if !ok {
		t.Fatalf("Did not receive %q message", m)
	}
	if msg.(string) != m {
		t.Fatal("Mailbox 2_2 is broken remotely.")
	}
}

func TestMessagesCanSendMailboxes(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	// Here we send address1_2 a reference to address1_1, from the POV
	// where address1_1 is local. Can we then use that to send to
	// address1_1 from the remote node?
	if err := ntb.fromNode2toNode1mailbox2.Send(ntb.node1address1); err != nil {
		t.Fatal(err)
	}
	sa, ok := ntb.node2mailbox1.ReceiveNextTimeout(timeout)
	if !ok {
		t.Fatal("No message received")
	}
	sentAddr, ok := sa.(*Address)
	if !ok {
		t.Fatal("Received message is not an Address")
	}

	// fixup the connectionServer
	sentAddr.connectionServer = ntb.node2connectionServer

	if err := sentAddr.Send("ack"); err != nil {
		t.Fatal(err)
	}
	received, ok := ntb.node1mailbox1.ReceiveNextTimeout(timeout)
	if !ok {
		t.Fatal("No message received")
	}
	if received.(string) != "ack" {
		t.Fatal("Can not marshal around mailbox references.")
	}
}

// Little tests which just get some coverage out of the way.
func TestCoverage(t *testing.T) {
	ntb := testbed(nil, testLogger{t})

	if !panics(func() { newConnections(nil, NodeID(1)) }) {
		t.Fatal("Didn't panic with bad newConnections: no cluster")
	}

	ntb.terminate()

	c := &Cluster{}
	if !panics(func() { newConnections(c, NodeID(1)) }) {
		t.Fatal("Didn't panic with bad newConnections: no clusterlogger")
	}

	// set a null cluster so that we've already declared one, then test
	// that we can't declare another.
	NoClustering(NullLogger)
	if !panics(func() { _, _, _ = createFromSpec(testSpec(), 10, NullLogger) }) {
		t.Fatal("createFromSpec does not object to double-creating connections")
	}

	setConnections(nil)

	_, _, err := createFromSpec(testSpec(), 10, NullLogger)
	if err != errNodeNotDefined {
		t.Fatal("Failed to verify the claimed local node is in the cluster")
	}
}

// There's the following basic situations for remote links:
// * We link the remote address, and unlink it before it terminates.
// * We link the remote address, and it terminates before we're done.
// * We link the remote address, and it terminates, but before the
//   message goes across, the link to the node terminates.

func TestHappyPathRemoteLink(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	// from the perspective of node 1, "Mr. Mailbox 1_2 on node 2,
	// please tell me when you terminate."
	ntb.fromNode2toNode1mailbox2.NotifyAddressOnTerminate(ntb.node1address1)
	// yes, we run it twice; this covers the duplicate code path
	ntb.fromNode2toNode1mailbox2.NotifyAddressOnTerminate(ntb.node1address1)

	// "Let's wait on our testing until 1_2 has successfully recorded
	// that 1_1 wants notification on termination.", which, due to
	// how the internals work, is actually done when the Node 2
	// remoteMailbox.Address is registered on mailbox 1_2.
	ntb.node2address1.getAddress().(*Mailbox).blockUntilNotifyStatus(ntb.node2remoteMailboxes.Address, true)

	// now that we know the listens are all set up "correctly", let's see
	// if we get the terminate.
	ntb.node2mailbox1.Terminate()
	termNotice, ok := ntb.node1mailbox1.ReceiveNextTimeout(timeout)
	if !ok {
		t.Fatal("No message received")
	}
	if MailboxID(termNotice.(MailboxTerminated)) != ntb.node2mailbox1.id {
		t.Fatal("Received a termination notice for the wrong mailbox")
	}
}

// This tests what happens if we have two notifications, and then
// unnotify one of them. We still want to receive the termination notice.
func TestHappyPathPartialUnnotify(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	// this is used to determine when the remote mailboxes have
	// successfully processed a unnotifyRemote on node1. As this
	// should produce no message sent to node 2 (since there will still
	// be a listener for the message), that's all we have to sync on.
	gotUnnotifyRemote := make(chan struct{})
	err := ntb.node1remoteMailboxes.Send(newDoneProcessing{func(x interface{}) bool {
		_, isUnnotifyRemote := x.(internal.UnnotifyRemote)
		if isUnnotifyRemote {
			gotUnnotifyRemote <- void
		}
		return !isUnnotifyRemote
	}})
	if err != nil {
		t.Fatal(err)
	}

	// Add notifications on 1_1 to both mailboxes on node 1
	ntb.fromNode2toNode1mailbox2.NotifyAddressOnTerminate(ntb.node1address1)
	ntb.fromNode2toNode1mailbox2.NotifyAddressOnTerminate(ntb.node1address2)
	ntb.node2address1.getAddress().(*Mailbox).blockUntilNotifyStatus(ntb.node2remoteMailboxes.Address, true)

	// remove it from one node
	ntb.fromNode2toNode1mailbox2.RemoveNotifyAddress(ntb.node1address1)

	<-gotUnnotifyRemote
	// now, ensure that we still get notified on the remaining address
	ntb.node2mailbox1.Terminate()
	termNotice, ok := ntb.node2mailbox1.ReceiveNextAsync()
	if !ok {
		t.Fatal("No message received")
	}
	if MailboxID(termNotice.(MailboxTerminated)) != ntb.node2mailbox1.id {
		t.Fatal("Did not receive the right termination notice:", termNotice)
	}
}

// This is like the previous test, except that we add two notifications
// and remove both of them.
func TestHappyPathFullUnnotify(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	ntb.fromNode2toNode1mailbox2.NotifyAddressOnTerminate(ntb.node1address1)
	ntb.fromNode2toNode1mailbox2.NotifyAddressOnTerminate(ntb.node1address2)

	// now, verify that the other side does indeed get a full Remove
	// command when we remove the other notify address
	gotRemoveNotifyNode := make(chan struct{})
	err := ntb.node2remoteMailboxes.Send(newDoneProcessing{func(x interface{}) bool {
		_, isRNNOT := x.(*internal.RemoveNotifyNodeOnTerminate)
		if isRNNOT {
			gotRemoveNotifyNode <- void
		}
		return !isRNNOT
	}})
	if err != nil {
		t.Fatal(err)
	}
	ntb.fromNode2toNode1mailbox2.RemoveNotifyAddress(ntb.node1address1)
	ntb.fromNode2toNode1mailbox2.RemoveNotifyAddress(ntb.node1address2)

	// if we got this far, then the unnotify got processed.
	<-gotRemoveNotifyNode
}

func TestRemoteLinkErrorPaths(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	setConnections(ntb.node1connectionServer)

	// Send the remoteMailbox a message for the wrong node. (Verified that
	// this goes down the right code path via coverage analysis.)
	err := ntb.node2remoteMailboxes.Send(&internal.IncomingMailboxMessage{
		Target:  internal.IntMailboxID(ntb.node1mailbox1.id),
		Message: "moo",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
}

func TestConnectionPanicsClient(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	c := make(chan struct{})
	ntb.node2remoteMailboxes.connectionEstablished = func() {
		c <- struct{}{}
	}

	err := ntb.node2remoteMailboxes.Send(internal.PanicHandler{})
	if err != nil {
		t.Fatal(err)
	}

	// this proves the connection was re-established.
	<-c
}

func TestConnectionPanicsServer(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	c := make(chan struct{})

	ntb.node1remoteMailboxes.Lock()
	ntb.node1remoteMailboxes.connectionEstablished = func() {
		c <- struct{}{}
	}
	ntb.node1remoteMailboxes.Unlock()

	err := ntb.node1remoteMailboxes.Send(internal.PanicHandler{})
	if err != nil {
		t.Fatal(err)
	}

	// this proves the connection was re-established.
	<-c
}

func TestConnectionDiesClient(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	c := make(chan struct{})
	ntb.node2remoteMailboxes.connectionEstablished = func() {
		c <- struct{}{}
	}

	err := ntb.node2remoteMailboxes.Send(internal.DestroyConnection{})
	if err != nil {
		t.Fatal(err)
	}

	<-c
}

func TestConnectionDiesServer(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	c := make(chan struct{})

	ntb.node1remoteMailboxes.Lock()
	ntb.node1remoteMailboxes.connectionEstablished = func() {
		c <- struct{}{}
	}
	ntb.node1remoteMailboxes.Unlock()

	err := ntb.node1remoteMailboxes.Send(internal.DestroyConnection{})
	if err != nil {
		t.Fatal(err)
	}

	<-c
}

func TestHeartbeatRoundtrip(t *testing.T) {
	ntb := testbed(nil, testLogger{t})
	defer ntb.terminate()

	// Set the peekFunc() on c1's node connection to c2 object.
	c := make(chan internal.ClusterMessage)
	ntb.node1connectionServer.nodeConnectors[2].connection.setPeekFunc(func(cm internal.ClusterMessage) { c <- cm })

	var recPing, recPong bool

	// Take a peek at the first two messages. The only messages sent between nodes of
	// this ntb should be alternating PING and PONG messages.  Make sure we get one
	// of each.  There may be an AllNodeClaims message in there as well.
	for i := 0; i < 3; i++ {
		switch cm := <-c; cm.(type) {
		case *internal.AllNodeClaims:
		case *internal.Ping:
			recPing = true
		case *internal.Pong:
			recPong = true
		default:
			t.Errorf("received unexpected message type: %#v", cm)
		}
	}

	switch {
	case recPing && !recPong:
		t.Error("received two PING messages but no PONG messages; the PingInterval may be too short")
	case !recPing && recPong:
		t.Error("received two PONG messages but no PING messages; something is very wrong")
	}
}

func TestCoverRemoteMailboxes(t *testing.T) {
	rm := new(remoteMailboxes)
	rm.ClusterLogger = NullLogger
	_ = rm.send(internal.PanicHandler{})
}

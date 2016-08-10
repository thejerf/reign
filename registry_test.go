package reign

import (
	"testing"

	"github.com/thejerf/reign/internal"
)

type FakeRegistryServer struct{}

func (f FakeRegistryServer) getNodes() []NodeID {
	return []NodeID{13, 37}
}

func (f FakeRegistryServer) newLocalMailbox() (Address, *Mailbox) {
	addr := Address{nil, nil, nil}
	return addr, nil
}

func (f FakeRegistryServer) AddConnectionStatusCallback(_ func(NodeID, bool)) {}

func TestNewRegistry(t *testing.T) {
	_ = newRegistry(FakeRegistryServer{}, NodeID(0))
}

func TestConnectionStatusCallback(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	r := newRegistry(cs, cs.nodeID)
	r.connectionStatusCallback(cs.nodeID, false)
	r.Terminate()
}

func TestLookup(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	r := newRegistry(cs, cs.nodeID)
	defer r.Terminate()
	name := "name"
	_ = r.Lookup(name)
}

func TestSendRegistryMessage(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	r := newRegistry(cs, cs.nodeID)
	defer r.Terminate()
	go func() { r.Serve() }()
	defer r.Stop()
	// This doesn't really do anything
	r.send(sendRegistryMessage{message: "TestSendRegistryMessage 123 123"})
}

func TestNotifyTerminateUnregistered(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	a, m := cs.NewMailbox()
	defer m.Terminate()
	r := newRegistry(cs, cs.nodeID)
	defer r.Terminate()
	go func() { r.Serve() }()

	name := "blah"

	rm := registryMailbox{
		name:             name,
		connectionServer: cs,
	}

	// This should send MailboxTerminated to the mailbox associated with a (i.e. m), since
	// name was never registered with the registry
	n := notifyOnTerminateRegistryAddr{mailbox: rm, addr: a}
	r.send(n)
	r.Stop()

	mTerm := m.ReceiveNext()
	if _, ok := mTerm.(MailboxTerminated); !ok {
		t.Fatal("Should have received a MailboxTerminated after registry is stoppeds")
	}
}

func TestUnregisterTermination(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	addr, mbx := cs.NewMailbox()
	defer mbx.Terminate()
	r := newRegistry(cs, cs.nodeID)
	defer r.Terminate()
	go func() { r.Serve() }()
	defer r.Stop()

	name := "blah"

	r.Register(name, addr)

	rm := registryMailbox{
		name:             name,
		connectionServer: cs,
	}

	n := notifyOnTerminateRegistryAddr{mailbox: rm, addr: addr}

	// Send twice, to test the case where the notification map does and doesn't already have
	// an entry for the mailbox
	r.send(n)
	r.send(n)

	// This ought to send a MailboxTerminated message to the mailbox associated with addr
	// (i.e. mbx)
	r.Unregister(name, addr)

	mTerm := mbx.ReceiveNext()
	if _, ok := mTerm.(MailboxTerminated); !ok {
		t.Fatal("Should have received a MailboxTerminated after registry is stopped")
	}
}

func TestRemoveOnTerminate(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	addr, mbx := cs.NewMailbox()
	defer mbx.Terminate()
	r := newRegistry(cs, cs.nodeID)
	defer r.Terminate()
	go func() { r.Serve() }()
	defer r.Stop()

	realName := "blah"
	fakeName := "blorg"

	// Register this name with this addr
	r.Register(realName, addr)

	// Notify addr when rm gets terminated
	realRegistryMailbox := registryMailbox{
		name:             realName,
		connectionServer: cs,
	}
	notify := notifyOnTerminateRegistryAddr{mailbox: realRegistryMailbox, addr: addr}
	r.send(notify)

	// Don't notify addr anymore when rm gets terminated
	removeNotify := removeNotifyOnTerminateRegistryAddr{mailbox: realRegistryMailbox, addr: addr}
	r.send(removeNotify)

	// This mailbox was never registered under fakeName
	fakeRegistryMailbox := registryMailbox{
		name:             fakeName,
		connectionServer: cs,
	}
	removeNotify = removeNotifyOnTerminateRegistryAddr{mailbox: fakeRegistryMailbox, addr: addr}
	// This should do nothing
	r.send(removeNotify)

	// This should not send a message to addr
	r.unregister(cs.nodeID, realName, mbx.id)

	// We need to make sure that mbx didn't receive a MailboxTerminated. Do this by sending it a void
	// message and popping from the message queue to make sure that void was the first message it received
	mbx.send(void)
	v := mbx.ReceiveNext()
	if _, ok := v.(voidtype); !ok {
		t.Fatal("Should not have received anything but voidtype after unsubscribing for termination notification")
	}
}

func TestConnectionStatus(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	addr, mbx := cs.NewMailbox()
	defer mbx.Terminate()
	r := newRegistry(cs, cs.nodeID)
	defer r.Terminate()
	go func() { r.Serve() }()
	defer r.Stop()

	// This shouldn't do anything, since connected == true
	s := connectionStatus{cs.nodeID, true}
	r.send(s)

	synch := make(chan voidtype)
	r.send(synchronizeRegistry{synch})
	<-synch

	// First register a mailbox
	name := "blah"
	r.Register(name, addr)

	// This should unregister everything on this node.
	s.connected = false
	r.send(s)
	r.send(synchronizeRegistry{synch})
	<-synch

	// Now make sure the mailbox was unregistered by re-registering with the same info (if unregister
	// was never called, this would panic)
	r.Register(name, addr)
	r.send(synchronizeRegistry{synch})
	<-synch
	mbx.send(void)

	// mbx should not have received a MultipleClaim
	v := mbx.ReceiveNext()
	if _, ok := v.(voidtype); !ok {
		t.Fatalf("Did not expect a %#v message: ", v)
	}
}

func TestInternalRegisterName(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	r := newRegistry(cs, cs.nodeID)
	defer r.Terminate()
	go func() { r.Serve() }()
	defer r.Stop()

	addr1, mbx1 := cs.NewMailbox()
	defer mbx1.Terminate()
	addr2, mbx2 := cs.NewMailbox()
	defer mbx2.Terminate()

	name := "blah"
	r.Register(name, addr1)
	r.Register(name, addr2)
	// 255 is nonexistent node, but we're only looking at the messages sent to mbx1. This
	// should trigger a MultiplClaim broadcast to mbx2 (invalid) and mbx1 (valid)
	synch := make(chan voidtype)
	r.send(synchronizeRegistry{synch})
	<-synch

	mbx1.send(void)
	mbx2.send(void)

	// Make sure we received the MultipleClaim
	mc := mbx1.ReceiveNext()
	if _, ok := mc.(MultipleClaim); !ok {
		t.Fatal("Register is supposed to trigger a register")
	}
	mc = mbx2.ReceiveNext()
	if _, ok := mc.(MultipleClaim); !ok {
		t.Fatal("Register is supposed to trigger a register")
	}
}

func TestInternalUnegisterName(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	r := newRegistry(cs, cs.nodeID)
	defer r.Terminate()
	go func() { r.Serve() }()
	defer r.Stop()

	name := "blah"
	fakeName := "blorg"

	addr, mbx := cs.NewMailbox()
	defer mbx.Terminate()
	err := r.Register(name, addr)

	if err != nil {
		t.Fatal("Initial Register should have succeeded")
	}

	synch := make(chan voidtype)
	r.send(synchronizeRegistry{synch})
	<-synch

	// If this Unregsister doesn't work, the second Register will trigger a MultipleClaim broadcast
	r.Unregister(name, addr)
	r.send(synchronizeRegistry{synch})
	<-synch
	mbx.send(void)

	// mbx should not have received a MultipleClaim
	v := mbx.ReceiveNext()
	if _, ok := v.(voidtype); !ok {
		t.Fatalf("Did not expect a %#v message: ", v)
	}

	// Unregistering a fake entry should do nothing
	r.Unregister(fakeName, addr)

	r.send(synchronizeRegistry{synch})
	<-synch
}

func TestNonGlobalRegisterable(t *testing.T) {
	// Attempting to send anything through this registry will panic, since
	// there is no connectionServer anywhere
	r := registry{}

	name := "blah"

	// Making addr.id = noMailbox means that canBeGloballyRegistered() == false,
	// So Register and Unregister should return immediately without trying to Send anything
	// through the registry
	addr := Address{}
	addr.id = noMailbox{mailboxID: mailboxID(1337)}

	// Need an error-checking closure, because Register might panic (though it shouldn't)
	f := func() {
		err := r.Register(name, addr)
		if err != ErrCantGloballyRegister {
			t.Fatal("Regsiter with a noMailbox AddressID should have failed")
		}
	}
	if panics(f) {
		t.Fatal("Register with a noMailbox should not panic")
	}

	// Should do nothing
	if panics(func() { r.Unregister(name, addr) }) {
		t.Fatal("Unregister with a noMailbox should not panic")
	}
}

func TestInternalAllNodeClaims(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	setConnections(cs)
	defer unsetConnections(t)
	r := newRegistry(cs, cs.nodeID)
	defer r.Terminate()
	go func() { r.Serve() }()
	defer r.Stop()

	// Make a bunch of names for the same mailbox and register them with AllNodeClaims
	names := []string{"foo", "bar", "baz"}
	_, mbx1 := cs.NewMailbox()
	defer mbx1.Terminate()
	mid := internal.IntMailboxID(mbx1.id)
	registrationMap := make(map[string]internal.IntMailboxID)
	for _, name := range names {
		registrationMap[name] = mid
	}
	anc := internal.AllNodeClaims{
		Node:   internal.IntNodeID(cs.nodeID),
		Claims: registrationMap,
	}
	r.send(anc)

	synch := make(chan voidtype)
	r.send(synchronizeRegistry{synch})
	<-synch

	// Now register all of the previous names again, but to a different mailbox.
	// This should trigger a MultipleClaim broadcast
	_, mbx2 := cs.NewMailbox()
	defer mbx2.Terminate()
	for _, name := range names {
		r.register(cs.nodeID, name, mbx2.id)
	}
	r.send(synchronizeRegistry{synch})
	<-synch

	// Make sure we don't block
	for _ = range names {
		mbx1.send(void)
	}
	for _ = range names {
		mbx2.send(void)
	}

	// Verify that we got MultipleClaims on both mailboxen
	for _, mbx := range []*Mailbox{mbx1, mbx2} {
		for _, name := range names {
			mc := mbx.ReceiveNext()
			if _, ok := mc.(MultipleClaim); !ok {
				t.Fatalf("Did not expect message of type %#v", mc)
			} else {
				if mc.(MultipleClaim).name != name {
					t.Fatalf("Expected name to be '%v'. Got '%v'", name, mc.(MultipleClaim).name)
				}
			}
		}
	}
}

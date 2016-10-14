package reign

import (
	"testing"

	"github.com/thejerf/reign/internal"
)

func TestConnectionStatusCallback(t *testing.T) {
	cs, r := noClustering(NullLogger)
	defer cs.Terminate()

	r.connectionStatusCallback(cs.nodeID, false)
}

func TestLookup(t *testing.T) {
	cs, r := noClustering(NullLogger)
	defer cs.Terminate()

	go func() { r.Serve() }()
	defer r.Stop()

	addr, mbx := cs.NewMailbox()
	defer mbx.Terminate()

	name := "684910"
	if err := r.Register(name, addr); err != nil {
		t.Fatal(err)
	}

	r.Sync()

	a := r.Lookup(name)
	a.Send(void)
	if _, ok := mbx.ReceiveNextAsync(); !ok {
		t.Fatal("No message received")
	}
}

func TestConnectionStatus(t *testing.T) {
	cs, r := noClustering(NullLogger)
	defer cs.Terminate()

	go func() { r.Serve() }()
	defer r.Stop()

	addr, mbx := cs.NewMailbox()
	defer mbx.Terminate()

	// This shouldn't do anything, since connected == true
	s := connectionStatus{cs.nodeID, true}
	r.send(s)

	// First register a mailbox
	name := "684910"
	if err := r.Register(name, addr); err != nil {
		t.Fatal(err)
	}

	// This should unregister everything on this node.
	s.connected = false
	r.send(s)
	r.Sync()

	// Now make sure the mailbox was unregistered by re-registering with the same info (if unregister
	// was never called, this would panic)
	if err := r.Register(name, addr); err != nil {
		t.Fatal(err)
	}
	r.Sync()
	addr.Send(void)

	// mbx should not have received a MultipleClaim
	v, ok := mbx.ReceiveNextAsync()
	if !ok {
		t.Fatal("No message received")
	}
	if _, ok := v.(voidtype); !ok {
		t.Fatalf("Did not expect a %t message: ", v)
	}
}

func TestNoRegistryServe(t *testing.T) {
	cs, _ := noClustering(NullLogger)
	defer cs.Terminate()

	addr, mbx := cs.NewMailbox()
	defer mbx.Terminate()

	addr.Send(void)
	if _, ok := mbx.ReceiveNextAsync(); !ok {
		t.Fatal("No message received")
	}
}

func TestUnregisterOnTerminate(t *testing.T) {
	cs, r := noClustering(NullLogger)
	defer cs.Terminate()

	go func() { r.Serve() }()
	defer r.Stop()

	addr, mbx := cs.NewMailbox()

	names := []string{"blah", "blech", "blorg"}

	for _, name := range names {
		if err := r.Register(name, addr); err != nil {
			t.Fatal(err)
		}
	}
	r.Sync()

	// Make sure Lookup works
	for _, name := range names {
		addr := r.Lookup(name)
		if addr == nil {
			t.Fatalf("Lookup failed on registered name '%s'", name)
		}
	}

	// Terminate() should call r.UnregisterMailbox and unregistered all the names that
	// belong to mbx
	mbx.Terminate()
	r.Sync()

	// Now make sure that all the names have been unregistered
	for _, name := range names {
		addr := r.Lookup(name)
		if addr != nil {
			t.Fatalf("Terminate() did not unregister name '%s'", name)
		}
	}
}

func TestInternalRegisterName(t *testing.T) {
	cs, r := noClustering(NullLogger)
	defer cs.Terminate()

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
	r.Sync()

	addr1.Send(void)
	addr2.Send(void)

	// Make sure we received the MultipleClaim
	mc, ok := mbx1.ReceiveNextAsync()
	if !ok {
		t.Fatal("No message received")
	}
	if _, ok = mc.(MultipleClaim); !ok {
		t.Fatal("Register is supposed to trigger a register")
	}
	mc, ok = mbx2.ReceiveNextAsync()
	if !ok {
		t.Fatal("No message received")
	}
	if _, ok = mc.(MultipleClaim); !ok {
		t.Fatal("Register is supposed to trigger a register")
	}
}

func TestInternalUnregisterName(t *testing.T) {
	cs, r := noClustering(NullLogger)
	defer cs.Terminate()

	go func() { r.Serve() }()
	defer r.Stop()

	name := "blah"
	fakeName := "blorg"

	addr, mbx := cs.NewMailbox()
	defer mbx.Terminate()

	if err := r.Register(name, addr); err != nil {
		t.Fatal("Initial Register should have succeeded")
	}

	r.Sync()

	// If this Unregsister doesn't work, the second Register will trigger a MultipleClaim broadcast
	r.Unregister(name, addr)
	r.Sync()
	addr.Send(void)

	// mbx should not have received a MultipleClaim
	v, ok := mbx.ReceiveNextAsync()
	if !ok {
		t.Fatal("No message received")
	}
	if _, ok := v.(voidtype); !ok {
		t.Fatalf("Did not expect a %#v message: ", v)
	}

	// Unregistering a fake entry should do nothing
	r.Unregister(fakeName, addr)
	r.Sync()
}

func TestInternalAllNodeClaims(t *testing.T) {
	cs, r := noClustering(NullLogger)
	defer cs.Terminate()

	go func() { r.Serve() }()
	defer r.Stop()

	// Make a bunch of names for the same mailbox and register them with AllNodeClaims
	names := []string{"foo", "bar", "baz"}

	addr1, mbx1 := cs.NewMailbox()
	defer mbx1.Terminate()

	mid := internal.IntMailboxID(mbx1.id)
	registrationMap := make(map[string]map[internal.IntMailboxID]struct{})
	for _, name := range names {
		registrationMap[name] = map[internal.IntMailboxID]struct{}{
			mid: struct{}{},
		}
	}
	anc := internal.AllNodeClaims{
		Node:   internal.IntNodeID(cs.nodeID),
		Claims: registrationMap,
	}
	r.send(anc)

	r.Sync()

	// Now register all of the previous names again, but to a different mailbox.
	// This should trigger a MultipleClaim broadcast
	addr2, mbx2 := cs.NewMailbox()
	defer mbx2.Terminate()

	for _, name := range names {
		if err := r.Register(name, addr2); err != nil {
			t.Fatal(err)
		}
	}
	r.Sync()

	// Make sure we don't block
	for range names {
		addr1.Send(void)
	}
	for range names {
		addr2.Send(void)
	}

	// Verify that we got MultipleClaims on both mailboxen
	for _, mbx := range []*Mailbox{mbx1, mbx2} {
		for _, name := range names {
			mc, ok := mbx.ReceiveNextAsync()
			if !ok {
				t.Fatalf("No message received for name %q", name)
			}
			if _, ok := mc.(MultipleClaim); !ok {
				t.Fatalf("Did not expect message of type %#v", mc)
			} else {
				if mc.(MultipleClaim).Name != name {
					t.Fatalf("Expected name to be '%v'. Got '%v'", name, mc.(MultipleClaim).Name)
				}
			}
		}
	}
}

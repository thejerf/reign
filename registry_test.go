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

	name := "684910"

	// Lookup an identier with no registered Addresses.
	a := r.Lookup(name)
	if a != nil {
		t.Fatalf("expected nil; actual: %#v", a)
	}

	addr1, mbx1 := cs.NewMailbox()
	defer mbx1.Terminate()

	if err := r.Register(name, addr1); err != nil {
		t.Fatal(err)
	}

	r.Sync()

	// Lookup an identifier with a single Address.
	a = r.Lookup(name)
	err := a.Send(void)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mbx1.ReceiveNextAsync(); !ok {
		t.Fatal("No message received")
	}

	addr2, mbx2 := cs.NewMailbox()
	defer mbx2.Terminate()

	addr3, mbx3 := cs.NewMailbox()
	defer mbx3.Terminate()

	if err := r.Register(name, addr2); err != nil {
		t.Fatal(err)
	}

	if err := r.Register(name, addr3); err != nil {
		t.Fatal(err)
	}

	r.Sync()

	// LookupAll should return a slice with 3 Addresses.
	all := r.LookupAll(name)
	if actual := len(all); actual != 3 {
		t.Fatalf("expected 3 Addresses; actual %d", actual)
	}

	// Make sure the given Address is one of the 3 we registered with the given name.
	check := func(t *testing.T, addr *Address) {
		switch addr.GetID() {
		case addr1.GetID(), addr2.GetID(), addr3.GetID():
		default:
			t.Fatal("got unexpected Address")
		}
	}

	for _, addr := range all {
		check(t, addr)
	}

	// Lookup an identifier with multiple Addresses.  A random Address should be returned.
	a = r.Lookup(name)
	check(t, a)
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
	if err := r.send(s); err != nil {
		t.Fatal(err)
	}

	// First register a mailbox
	name := "684910"
	if err := r.Register(name, addr); err != nil {
		t.Fatal(err)
	}

	// This should unregister everything on this node.
	s.connected = false
	if err := r.send(s); err != nil {
		t.Fatal(err)
	}
	r.Sync()

	// Now make sure the mailbox was unregistered by re-registering with the same info (if unregister
	// was never called, this would panic)
	if err := r.Register(name, addr); err != nil {
		t.Fatal(err)
	}
	r.Sync()
	if err := addr.Send(void); err != nil {
		t.Fatal(err)
	}

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

	if err := addr.Send(void); err != nil {
		t.Fatal(err)
	}
	if _, ok := mbx.ReceiveNextAsync(); !ok {
		t.Fatal("No message received")
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
	if err := r.Register(name, addr1); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(name, addr2); err != nil {
		t.Fatal(err)
	}
	// 255 is nonexistent node, but we're only looking at the messages sent to mbx1. This
	// should trigger a MultiplClaim broadcast to mbx2 (invalid) and mbx1 (valid)
	r.Sync()

	if err := addr1.Send(void); err != nil {
		t.Fatal(err)
	}
	if err := addr2.Send(void); err != nil {
		t.Fatal(err)
	}

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
	if err := addr.Send(void); err != nil {
		t.Fatal(err)
	}

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

	go r.Serve()
	defer r.Stop()

	// Make a bunch of names for the same mailbox and register them with AllNodeClaims
	names := []string{"foo", "bar", "baz"}

	addr1, mbx1 := cs.NewMailbox()
	defer mbx1.Terminate()

	mID := internal.IntMailboxID(mbx1.id)
	registrationMap := make(map[string]map[internal.IntMailboxID]struct{})
	for _, name := range names {
		registrationMap[name] = map[internal.IntMailboxID]struct{}{
			mID: struct{}{},
		}
	}
	anc := &internal.AllNodeClaims{
		Node:   internal.IntNodeID(cs.nodeID),
		Claims: registrationMap,
	}
	if err := r.send(anc); err != nil {
		t.Fatal(err)
	}

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
		if err := addr1.Send(void); err != nil {
			t.Fatal(err)
		}
	}
	for range names {
		if err := addr2.Send(void); err != nil {
			t.Fatal(err)
		}
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

func TestMultipleClaimCount(t *testing.T) {
	cs, r := noClustering(NullLogger)
	defer cs.Terminate()

	go func() { r.Serve() }()
	defer r.Stop()

	addr1, mbx1 := cs.NewMailbox()
	defer mbx1.Terminate()

	addr2, mbx2 := cs.NewMailbox()
	defer mbx2.Terminate()

	addr3, mbx3 := cs.NewMailbox()
	defer mbx3.Terminate()

	addr4, mbx4 := cs.NewMailbox()
	defer mbx4.Terminate()

	addr5, mbx5 := cs.NewMailbox()
	defer mbx5.Terminate()

	if actual := r.MultipleClaimCount(); actual != 0 {
		t.Fatalf("expected 0 multiple claims; actual = %d", actual)
	}

	name1 := "foo"
	if err := r.Register(name1, addr1); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(name1, addr2); err != nil {
		t.Fatal(err)
	}
	r.Sync()

	// There is a multiple claim for name1.
	if actual := r.MultipleClaimCount(); actual != 1 {
		t.Fatalf("expected 1 multiple claim; actual = %d", actual)
	}

	name2 := "bar"
	if err := r.Register(name2, addr3); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(name2, addr4); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(name2, addr5); err != nil {
		t.Fatal(err)
	}
	r.Sync()

	// There are now two multiple claims, one for name1 and one for name2.
	if actual := r.MultipleClaimCount(); actual != 2 {
		t.Fatalf("expected 2 multiple claims; actual = %d", actual)
	}

	r.Unregister(name1, addr2)
	r.Sync()

	// We resolved one of the multiple claims.
	if actual := r.MultipleClaimCount(); actual != 1 {
		t.Fatalf("expected 1 multiple claim; actual = %d", actual)
	}

	r.Unregister(name2, addr3)
	r.Sync()

	// We should *still* have 1 multiple claim because there were 3 mailboxes
	// registered under name2.
	if actual := r.MultipleClaimCount(); actual != 1 {
		t.Fatalf("expected 1 multiple claim; actual = %d", actual)
	}

	r.Unregister(name2, addr5)
	r.Sync()

	// Both name1 and name2 should only have a single claim now.
	if actual := r.MultipleClaimCount(); actual != 0 {
		t.Fatalf("expected 0 multiple claims; actual = %d", actual)
	}
}

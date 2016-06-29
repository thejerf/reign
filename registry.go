package reign

/*

This implements a global string -> Address registry.

This implementation does something a little different than you may be
used to when it comes to distributed systems. One of the most difficult
problems in distributed systems is that of *consistency*... how do you
know that both Node A and Node B agree that the value of some setting
that they can both change is X and not Y? What does it even *mean*
for them to agree that it is X and not Y in the light of the fact that
they can't even agree what time it is and so "see Y at the same time"
is intrinsically difficult to define?

This implementation zigs where almost everything else on Earth zags.
We implement a name registry such that anything in the cluster can
register that it owns a name, and this will propagate globally across
the cluster. We then *completely punt on consistency*. That is, two
things are *welcome* by the system to claim the same name.

What happens if they do that? Well, the system simply takes the message
you are sending, and sends it to *both* registrants... or, more accurately,
*all* the registrants.

What? Chaos! Madness! Insanity! ... actually, in a lot of these distributed
systems, not so much. Erlang-style message passing already only promises
a best-effort delivery standing between 0 and 1 copies of the message
delivered; it isn't that much more crazy to then say that if you invoke
certain functionality you may also get N deliveries. One example of
how this doesn't matter is if you have a service being provided by
an external resource that connects into the cluster and registers itself
by some name, reconnecting when it is disconnected; if the remote service
itself never has more than one simultaneous connection open, then the
remote service is implementing the constraint that there can not possibly
be more than one correct address.

In return, what you get is Total Partitionability. Unlike many other
systems that fail if a certain number of nodes drop out of the cluster,
a Reign cluster will carry on to the best of its ability using whatever
local resources are available. When the cluster returns, so will the
whatever other remote resources are supposed to be available. It is
sometimes claimed that "partitions are uncommon"... well, that entirely
depends on scale. This makes reign scale to global scale; a Reign
cluster is designed to be globally distributed if you like. If two
datacenters lose their connectivity, they can continue to service what
they can, and transparently knit back together when the connection comes
back. Of course, whether the system you are implementing can handle
that is up to you....

As a final note, when the notification of a claim of an Address that
something local also believes is claimed comes in, a notification to
the local claimant will be sent telling them that there is a conflicting
claim, including all claimants the local node believes to exist. In my
case, since I do indeed have services registering themselves with a
service socket, I just kill whatever socket I think I have in response to
such a claim. If the local service is the "real" service, it'll issue
another claim again on that name; if it is fake, then my local registration
just goes away.

*/

import (
	"encoding/json"
	"errors"
	"math/rand"
	"sync"

	"github.com/thejerf/reign/internal"
)

// ErrNoAddressRegistered is returned when there are no addresses at
// the given name.
var ErrNoAddressRegistered = errors.New("no address is registered with that name")

// ErrCantGloballyRegister is returned when you are trying to register
// an address with the registry that can not be so registered. Only local
// mailboxes created with New() can be registered with the registry.
var ErrCantGloballyRegister = errors.New("can't globally register this address")

// MultipleClaim messages are send to name claimers in the event that
// there are multiple name claims to a given name. Note that the claimants
// array is a static snapshot of the claimants at the time of conflict,
// and that the "current" situation (to the extent that is definable)
// may change at any time.
type MultipleClaim struct {
	claimants []Address
	name      string
}

type stopRegistry struct{}

// This is used for unit testing
type synchronizeRegistry struct {
	ch chan voidtype
}

// IMPORTANT: Do not call the private (lowercase) methods of registry without taking out the lock (registry.m)
// Most of the functionality required can be accessed through the publically-exposed (uppercase) methods
type registry struct {
	// the set of all claims understood by the local node, organized as
	// name -> set of mailbox IDs.
	claims map[string]map[MailboxID]voidtype

	// The map of nodes -> addresses used to communicate with those nodes.
	// When a registration is made, this is the list of nodes that will
	// receive notifications of the new address. This only has remote nodes.
	nodeRegistries map[NodeID]Address

	m sync.Mutex

	*Address
	*Mailbox

	thisNode NodeID
}

type connectionStatus struct {
	node      NodeID
	connected bool
}

// This abstracts out the connectionServer for the registry, allowing it to
// be tested without setting up a full massive cluster.
type registryServer interface {
	getNodes() []NodeID
	newLocalMailbox() (Address, *Mailbox)
	AddConnectionStatusCallback(f func(NodeID, bool))
}

// NamesDebugger is an interface over the registry struct. These functions acquire locks and
// are not supposed to be called in a production setting.
type NamesDebugger interface {
	AddressCount() uint
	AllNames() []string
	DumpClaims() map[string][]MailboxID
	DumpJSON() string
	SeenNames(...string) []bool
}

// Names exposes some functionality of registry
//
// Lookup looks up a given name and returns a mailbox that can be used to
// send messages and request termination notifications.
//
// Be sure to consult the documentation about the Registry in the
// documentation section above; use of this address could in some
// circumstances result in the message being delivered to more than one
// Mailbox.
//
// In particular, this function does no checking as to whether the address
// exists, as that information is intrinsically racy anyhow. If you want to
// take extra care about it, use NotifyAddressOnTerminate, just as with
// local addresses.
//
// Register claims the given global name in the registry. It can then be
// accessed and manipulated via Lookup.
//
// This does not happen synchronously, as there seems to be no reason
// for the caller to synchronously wait for this.
//
// A registered mailbox should stand ready to receive MultipleClaim
// messages from the cluster.
//
// The passed-in Address must be something that directly came from a New()
// call. Addressed obtained from the network or from Lookup itself will
// return an error instead of registering.
//
//
// Unregister removes the given claim from a given global name.
// Unregistration will only occur if the current registrant matches the
// address passed in. It is not an error for it not to match; the call will
// simply be ignored.
//
// On a given node, only one Address can have a claim on a
// name. If you wish to supercede a claim with a new address, you can
// simply register the new claim, and it will overwrite the previous one.
//
// If the address passed in is not the current registrant, the call is
// ignored, thus it is safe to call this.
type Names interface {
	GetDebugger() NamesDebugger
	Lookup(string) *Address
	Register(string, *Address) error
	SeenNames(...string) []bool
	Sync()
	Unregister(string, *Address)
}

// conceptually, I consider this inline with the newConnections function,
// since this deeply depends on the order of parameters initialized in the
// &connectionServer{} in an otherwise icky way...
func newRegistry(server *connectionServer, node NodeID) *registry {
	r := &registry{
		claims:         make(map[string]map[MailboxID]voidtype),
		nodeRegistries: make(map[NodeID]Address),
		thisNode:       node,
	}

	r.Address, r.Mailbox = server.newLocalMailbox()
	r.Address.connectionServer = server

	server.AddConnectionStatusCallback(r.connectionStatusCallback)

	return r
}

func (r *registry) Terminate() {
	r.Mailbox.Terminate()
}

func (r *registry) connectionStatusCallback(node NodeID, connected bool) {
	_ = r
	r.Send(connectionStatus{node, connected})
}

func (r *registry) Stop() {
	r.Send(stopRegistry{})
}

func (r *registry) Serve() {
	for {
		m := r.ReceiveNext()
		switch msg := m.(type) {
		case internal.RegisterName:
			r.m.Lock()
			r.register(NodeID(msg.Node), msg.Name, MailboxID(msg.MailboxID))

			if NodeID(msg.Node) == r.thisNode {
				r.toOtherNodes(msg)
			}
			r.m.Unlock()

		case internal.UnregisterName:
			r.m.Lock()
			r.unregister(NodeID(msg.Node), msg.Name, MailboxID(msg.MailboxID))

			if NodeID(msg.Node) == r.thisNode {
				r.toOtherNodes(msg)
			}
			r.m.Unlock()

		// This should only be called internally
		case internal.UnregisterMailbox:
			r.m.Lock()
			r.unregisterMailbox(NodeID(msg.Node), MailboxID(msg.MailboxID))
			r.m.Unlock()

		case connectionStatus:
			r.m.Lock()
			// HERE: Handling this and the errors in mailbox.go
			r.handleConnectionStatus(msg)
			r.m.Unlock()

		case internal.AllNodeClaims:
			r.m.Lock()
			r.handleAllNodeClaims(msg)
			r.m.Unlock()

		case synchronizeRegistry:
			msg.ch <- void

		case stopRegistry:
			return
		}
	}
}

func (r *registry) Sync() {
	synch := make(chan voidtype)
	r.send(synchronizeRegistry{synch})
	<-synch
}

func (r *registry) handleAllNodeClaims(msg internal.AllNodeClaims) {
	node := NodeID(msg.Node)
	for name, intMailbox := range msg.Claims {
		r.register(node, name, MailboxID(intMailbox))
	}
}

// Handle connection status deals with the connection to a node going up or
// down.
//
// It turns out we don't really care when a node comes up; we expect the
// semantics of the clustering to just carry us through until the node is
// up. But when the connection goes down, we do need to unregister all
// the claims on that node
func (r *registry) handleConnectionStatus(msg connectionStatus) {
	if msg.connected == true {
		return
	}

	// TODO: make this more efficient?
	for name, claimants := range r.claims {
		for claimant := range claimants {
			if claimant.NodeID() == msg.node {
				r.unregister(msg.node, name, claimant)
			}
		}
	}

	delete(r.nodeRegistries, msg.node)
}

func (r *registry) toOtherNodes(msg interface{}) {
	for _, addr := range r.nodeRegistries {
		addr.Send(msg)
	}
}

func (r *registry) Lookup(s string) *Address {
	r.m.Lock()
	defer r.m.Unlock()

	claims := r.claims[s]
	claimIDs := make([]MailboxID, 0, len(claims))
	for k := range claims {
		claimIDs = append(claimIDs, k)
	}

	// If there is nothing in the registry with the given name, return nil
	if len(claimIDs) == 0 {
		return nil
	}

	// Pick a random ID from our list of IDs registered to this name and return it
	id := claimIDs[rand.Intn(len(claims))]
	return &Address{
		mailboxID:        MailboxID(id),
		connectionServer: r.connectionServer,
		mailbox:          nil,
	}
}

func (r *registry) Register(name string, addr *Address) error {
	if !addr.canBeGloballyRegistered() {
		return ErrCantGloballyRegister
	}

	// only mailboxID can pass the canBeGloballyRegistered test above
	r.Send(internal.RegisterName{
		Node:      internal.IntNodeID(r.thisNode),
		Name:      name,
		MailboxID: internal.IntMailboxID(addr.mailboxID),
	})
	return nil
}

func (r *registry) Unregister(name string, addr *Address) {
	if !addr.canBeGloballyRegistered() {
		return
	}

	// at the moment, only mailboxID can pass the check above
	r.Send(internal.UnregisterName{
		Node:      internal.IntNodeID(r.thisNode),
		Name:      name,
		MailboxID: internal.IntMailboxID(addr.mailboxID),
	})
}

// Unregisters all names that belong to the given mailbox. This may only be done locally.
// It will subsequently call unregister for every name associated with mID
func (r *registry) UnregisterMailbox(node NodeID, mID MailboxID) {
	if mID.NodeID() == node && node == r.thisNode {
		r.Send(internal.UnregisterMailbox{
			Node:      internal.IntNodeID(node),
			MailboxID: internal.IntMailboxID(mID),
		})
	}
}

func (r *registry) GetDebugger() NamesDebugger {
	return r
}

// This is the internal registration function.
func (r *registry) register(node NodeID, name string, mID MailboxID) {
	nameClaimants, haveNameClaimants := r.claims[name]
	if !haveNameClaimants {
		nameClaimants = map[MailboxID]voidtype{}
		r.claims[name] = nameClaimants
	}

	nameClaimants[mID] = void

	// If there are multiple claims now, and one of them is local,
	// notify our local Address of the conflict.
	if len(nameClaimants) > 1 && mID.NodeID() == r.thisNode {
		claimants := []Address{}
		for claimant := range nameClaimants {
			var addr Address
			addr.mailboxID = claimant
			addr.connectionServer = r.connectionServer
			claimants = append(claimants, addr)
		}
		for _, addr := range claimants {
			addr.Send(MultipleClaim{claimants, name})
		}
	}
}

// this is the internal unregistration function. In the event that this
// results in the last registry for a given name being removed, we check
// for anyone currently waiting for a termination notice on that name
// and send it.
func (r *registry) unregister(node NodeID, name string, mID MailboxID) {
	currentRegistrants, ok := r.claims[name]
	if !ok {
		// TODO: log error here
		return
	}
	delete(currentRegistrants, mID)

	if len(currentRegistrants) == 0 {
		delete(r.claims, name)
	}
}

// this unregisters all names associated with the given mailbox ID
func (r *registry) unregisterMailbox(node NodeID, mID MailboxID) {
	// TODO: make this more efficient?
	for name, claimants := range r.claims {
		for claimant := range claimants {
			if claimant == mID {
				r.unregister(node, name, claimant)
			}
		}
	}
}

// RegistryDebugger methods

func (r *registry) DumpClaims() map[string][]MailboxID {
	r.m.Lock()
	defer r.m.Unlock()

	copy := make(map[string][]MailboxID, len(r.claims))
	for name := range r.claims {
		addrs := make([]MailboxID, 0, len(r.claims[name]))
		for addr := range r.claims[name] {
			addrs = append(addrs, addr)
		}
		copy[name] = addrs
	}

	return copy
}

func (r *registry) DumpJSON() string {
	copy := r.DumpClaims()
	j, _ := json.Marshal(copy)
	return string(j)
}

func (r *registry) AddressCount() uint {
	r.m.Lock()
	defer r.m.Unlock()

	count := uint(0)
	for _, addresses := range r.claims {
		count += uint(len(addresses))
	}
	return count
}

func (r *registry) AllNames() []string {
	r.m.Lock()
	defer r.m.Unlock()

	names := make([]string, 0, len(r.claims))
	for name := range r.claims {
		names = append(names, name)
	}
	return names
}

// SeenNames returns an array where each element corresponds to whether the inputted name
// at that index was found in the registry or not
func (r *registry) SeenNames(names ...string) []bool {
	r.m.Lock()
	defer r.m.Unlock()

	seen := make([]bool, len(names))
	for _, name := range names {
		_, ok := r.claims[name]
		seen = append(seen, ok)
	}
	return seen
}

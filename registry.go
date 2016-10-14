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
	"fmt"
	"math/rand"
	"sync"

	"github.com/thejerf/reign/internal"
)

func init() {
	var mc MultipleClaim
	RegisterType(mc)
}

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

// synchronizeRegistry is used for unit testing
type synchronizeRegistry struct {
	ch chan voidtype
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
	Serve()
	Stop()
	Sync()
	Unregister(string, *Address)
}

// IMPORTANT: Do not call the private (lowercase) methods of registry without
// taking out the lock (registry.m).  Most of the functionality required can
// be accessed through the publically-exposed (uppercase) methods.
type registry struct {
	mu sync.Mutex

	// the set of all claims understood by the local node, organized as
	// name -> set of mailbox IDs.
	claims map[string]map[MailboxID]voidtype

	// The map of nodes -> addresses used to communicate with those nodes.
	// When a registration is made, this is the list of nodes that will
	// receive notifications of the new address. This only has remote nodes.
	nodeRegistries map[NodeID]Address

	*Address
	*Mailbox

	thisNode NodeID
}

type connectionStatus struct {
	node      NodeID
	connected bool
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

// conceptually, I consider this inline with the newConnections function,
// since this deeply depends on the order of parameters initialized in the
// &connectionServer{} in an otherwise icky way...
func newRegistry(cs *connectionServer, node NodeID) *registry {
	if cs == nil {
		panic("registry connection server cannot be nil")
	}

	r := &registry{
		claims:         make(map[string]map[MailboxID]voidtype),
		nodeRegistries: make(map[NodeID]Address),
		thisNode:       node,
	}

	r.Address, r.Mailbox = cs.newLocalMailbox()
	r.Address.connectionServer = cs

	cs.AddConnectionStatusCallback(r.connectionStatusCallback)

	return r
}

func (r *registry) Terminate() {
	r.Mailbox.Terminate()
}

// AddNodeRegistry acceptes a node ID and an Address, and adds the Address to the
// nodeRegistries map for the given node ID.  If the remote node's registry Address
// is not added to this map, this node will be unable to send registry-related
// messages to the remote node (e.g., register name, unregister name, etc.).
func (r *registry) addNodeRegistry(n NodeID, a Address) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nodeRegistries[n] = a
}

func (r *registry) connectionStatusCallback(node NodeID, connected bool) {
	r.Send(connectionStatus{node, connected})
}

func (r *registry) Stop() {
	r.Send(stopRegistry{})
}

func (r *registry) String() string {
	// Since the registry's Serve() method acquires a lock receiving messages,
	// we need to make sure that we also acquire that lock before replying to
	// Suture's service name inquiry.
	r.mu.Lock()
	defer r.mu.Unlock()

	return fmt.Sprintf("registry on node %d", r.thisNode)
}

func (r *registry) Serve() {
	for {
		message := r.ReceiveNext()
		switch msg := message.(type) {
		case internal.RegisterName: // Received locally
			r.register(msg.Name, MailboxID(msg.MailboxID))

			if NodeID(msg.Node) == r.thisNode {
				r.toOtherNodes(msg)
			}

		case *internal.RegisterName: // Received over the socket, decoded by gob
			r.register(msg.Name, MailboxID(msg.MailboxID))

		case internal.UnregisterName:
			r.unregister(msg.Name, MailboxID(msg.MailboxID))

			if NodeID(msg.Node) == r.thisNode {
				r.toOtherNodes(msg)
			}

		case *internal.UnregisterName:
			r.unregister(msg.Name, MailboxID(msg.MailboxID))

			// This should only be called internally
		case internal.UnregisterMailbox:
			r.unregisterMailbox(MailboxID(msg.MailboxID))

		case connectionStatus:
			// HERE: Handling this and the errors in mailbox.go
			r.handleConnectionStatus(msg)

		case internal.AllNodeClaims:
			r.handleAllNodeClaims(msg)

		case synchronizeRegistry:
			msg.ch <- void

		case stopRegistry:
			return

		case MailboxTerminated:
			// (Adam): The only scenario I've seen thus far where we receive a
			// MailboxTerminated is when the registry's mailbox is terminated and
			// the UnregisterMailbox message is sent to itself.  It may be more
			// proper to prevent the UnregisterMailbox message from going out to
			// begin with rather than catching the MailboxTerminated here.

		default:
			r.connectionServer.ClusterLogger.Errorf("Unknown registry message of type %T: %#v\n", msg, message)
		}
	}
}

func (r *registry) Sync() {
	synch := make(chan voidtype)
	r.send(synchronizeRegistry{synch})
	<-synch
}

// generateAllNodeClaims returns a populated AllNodeClaims object suitable
// for synchronizing mailbox claims with a remote node.
func (r *registry) generateAllNodeClaims() internal.AllNodeClaims {
	r.mu.Lock()
	defer r.mu.Unlock()

	anc := internal.AllNodeClaims{
		Node:   internal.IntNodeID(r.connectionServer.nodeID),
		Claims: make(map[string]map[internal.IntMailboxID]struct{}),
	}

	for name, mailboxIDs := range r.claims {
		if _, ok := anc.Claims[name]; !ok {
			anc.Claims[name] = make(map[internal.IntMailboxID]struct{})
		}

		for intMailbox := range mailboxIDs {
			anc.Claims[name][internal.IntMailboxID(intMailbox)] = struct{}{}
		}
	}

	return anc
}

func (r *registry) handleAllNodeClaims(msg internal.AllNodeClaims) {
	for name, mailboxIDs := range msg.Claims {
		for intMailboxID := range mailboxIDs {
			r.register(name, MailboxID(intMailboxID))
		}
	}
}

// handleConnectionStatus deals with the connection to a node going up or
// down.
//
// It turns out we don't really care when a node comes up; we expect the
// semantics of the clustering to just carry us through until the node is
// up. But when the connection goes down, we do need to unregister all
// the claims on that node.
func (r *registry) handleConnectionStatus(msg connectionStatus) {
	if msg.connected {
		return
	}

	// TODO: make this more efficient?
	for name, claimants := range r.claims {
		for claimant := range claimants {
			if claimant.NodeID() == msg.node {
				r.unregister(name, claimant)
			}
		}
	}

	r.mu.Lock()
	delete(r.nodeRegistries, msg.node)
	r.mu.Unlock()
}

func (r *registry) toOtherNodes(msg interface{}) {
	for _, addr := range r.nodeRegistries {
		addr.Send(msg)
	}
}

func (r *registry) GetDebugger() NamesDebugger {
	return r
}

// Lookup returns an Address that can be used to send to the mailboxes
// registered with the given string.
//
func (r *registry) Lookup(s string) *Address {
	r.mu.Lock()
	defer r.mu.Unlock()

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
		mailboxID:        id,
		connectionServer: r.connectionServer,
		mailbox:          nil,
	}
}

// Register claims the given global name in the registry.
//
// This does not happen synchronously, as there seems to be no reason
// for the caller to synchronously wait for this.
//
// A registered mailbox should stand ready to receive MultipleClaim
// messages from the cluster.
func (r *registry) Register(name string, addr *Address) error {
	if !addr.canBeGloballyRegistered() {
		return ErrCantGloballyRegister
	}

	// only mailboxID can pass the canBeGloballyRegistered test above
	return r.Send(internal.RegisterName{
		Node:      internal.IntNodeID(r.thisNode),
		Name:      name,
		MailboxID: internal.IntMailboxID(addr.mailboxID),
	})
}

// Unregister removes the given claim from a given global name.
// Unregistration will only occur if the current registrant matches the
// address passed in. It is not an error for it not to match; the call will
// simply be ignored.
//
// On a given node, only one Address can have a claim on a
// name. If you wish to supercede a claim with a new address, you can
// simply register the new claim, and it will overwrite the previous one.
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

// UnregisterMailbox unregisters all names that belong to the given mailbox.
// This may only be done locally.  It will subsequently call unregister() for
// every name associated with mID.
func (r *registry) UnregisterMailbox(node NodeID, mID MailboxID) {
	if mID.NodeID() == node && node == r.thisNode && r.mailboxID != mID {
		r.Send(internal.UnregisterMailbox{
			Node:      internal.IntNodeID(node),
			MailboxID: internal.IntMailboxID(mID),
		})
	}
}

// register is the internal registration function.
func (r *registry) register(name string, mID MailboxID) {
	r.mu.Lock()
	defer r.mu.Unlock()

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
			addr := Address{
				mailboxID:        claimant,
				connectionServer: r.connectionServer,
			}
			claimants = append(claimants, addr)
		}
		for _, addr := range claimants {
			addr.Send(MultipleClaim{claimants, name})
		}
	}
}

// unregister is the internal unregistration function. In the event that
// this results in the last registry for a given name being removed, we
// check for anyone currently waiting for a termination notice on that
// name and send it.
func (r *registry) unregister(name string, mID MailboxID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	currentRegistrants, ok := r.claims[name]
	if !ok {
		return
	}
	delete(currentRegistrants, mID)

	if len(currentRegistrants) == 0 {
		delete(r.claims, name)
	}
}

// unregisterMailbox unregisters all names associated with the given mailbox ID
func (r *registry) unregisterMailbox(mID MailboxID) {
	// TODO: make this more efficient?
	for name, claimants := range r.claims {
		for claimant := range claimants {
			if claimant == mID {
				r.unregister(name, claimant)
			}
		}
	}
}

// RegistryDebugger methods

func (r *registry) DumpClaims() map[string][]MailboxID {
	r.mu.Lock()
	defer r.mu.Unlock()

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

func (r *registry) AddressCount() (count uint) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, addresses := range r.claims {
		count += uint(len(addresses))
	}

	return
}

func (r *registry) AllNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, 0, len(r.claims))
	for name := range r.claims {
		names = append(names, name)
	}
	return names
}

// SeenNames returns an array where each element corresponds to whether the inputted name
// at that index was found in the registry or not
func (r *registry) SeenNames(names ...string) []bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	seen := make([]bool, 0, len(names))
	for _, name := range names {
		_, ok := r.claims[name]
		seen = append(seen, ok)
	}
	return seen
}

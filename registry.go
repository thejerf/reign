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
	"sync/atomic"
	"time"

	"github.com/thejerf/reign/internal"
)

func init() {
	var mc MultipleClaim
	RegisterType(&mc)

	rand.Seed(time.Now().UnixNano())
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
	Name string
}

type stopRegistry struct{}

// synchronizeRegistry is used for registry mailbox synchronization.
type synchronizeRegistry struct {
	ch chan voidtype
}

type registryEntry struct {
	name      string
	mailboxID MailboxID
}

type registryEntries []registryEntry

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
	LookupAll(string) []*Address
	MessageCount() int
	MultipleClaimCount() int32
	Register(string, *Address) error
	SeenNames(...string) []bool
	Sync()
	Unregister(string, *Address)
}

var _ Names = (*registry)(nil)

// IMPORTANT: Do not call the private (lowercase) methods of registry without
// taking out the lock (registry.m).  Most of the functionality required can
// be accessed through the publically-exposed (uppercase) methods.
type registry struct {
	ClusterLogger

	*Address
	*Mailbox

	thisNode NodeID

	// multipleClaimCount gauges the number of multiple claims.
	multipleClaimCount int32

	mu sync.RWMutex

	// the set of all claims understood by the local node, organized as
	// name -> set of mailbox IDs.
	claims map[string]map[MailboxID]voidtype

	// The set of all mailboxIDs understood by the local node, organized
	// as mailbox ID -> registryEntries, suitable for unregistering the
	// mailbox ID from each registered name.
	mailboxIDs map[MailboxID]registryEntries

	// The node registries map gets its own mutex since the map isn't involved
	// in other registry functions.  It's only concerned with notification to
	// other nodes in the cluster.
	regMu sync.RWMutex

	// The map of nodes -> addresses used to communicate with those nodes.
	// When a registration is made, this is the list of nodes that will
	// receive notifications of the new address. This only has remote nodes.
	nodeRegistries map[NodeID]Address
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
	MessageCount() int
	MultipleClaimCount() int32
	SeenNames(...string) []bool
}

// conceptually, I consider this inline with the newConnections function,
// since this deeply depends on the order of parameters initialized in the
// &connectionServer{} in an otherwise icky way...
func newRegistry(cs *connectionServer, node NodeID, log ClusterLogger) *registry {
	if cs == nil {
		panic("registry connection server cannot be nil")
	}

	r := &registry{
		claims:         make(map[string]map[MailboxID]voidtype),
		mailboxIDs:     make(map[MailboxID]registryEntries),
		nodeRegistries: make(map[NodeID]Address),
		thisNode:       node,
		ClusterLogger:  log,
	}

	r.Address, r.Mailbox = cs.newLocalMailbox()
	r.Address.connectionServer = cs

	cs.AddConnectionStatusCallback(r.connectionStatusCallback)

	return r
}

func (r *registry) Terminate() {
	r.Tracef("Terminating registry on node %d", r.thisNode)
	r.Mailbox.Terminate()
}

// AddNodeRegistry acceptes a node ID and an Address, and adds the Address to the
// nodeRegistries map for the given node ID.  If the remote node's registry Address
// is not added to this map, this node will be unable to send registry-related
// messages to the remote node (e.g., register name, unregister name, etc.).
func (r *registry) addNodeRegistry(n NodeID, a Address) {
	r.regMu.Lock()
	defer r.regMu.Unlock()

	if r.nodeRegistries == nil {
		r.nodeRegistries = make(map[NodeID]Address)
	}

	r.nodeRegistries[n] = a
	r.Tracef("Added address %q on node %d to the node registry", a.String(), n)
}

func (r *registry) connectionStatusCallback(node NodeID, connected bool) {
	_ = r.Send(connectionStatus{node, connected})
}

func (r *registry) Stop() {
	_ = r.Send(stopRegistry{})
}

func (r *registry) String() string {
	// Since the registry's Serve() method acquires a lock receiving messages,
	// we need to make sure that we also acquire that lock before replying to
	// Suture's service name inquiry.
	r.mu.RLock()
	defer r.mu.RUnlock()

	return fmt.Sprintf("Registry on node %d", r.thisNode)
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

		case internal.UnregisterMailbox: // This should only be called internally
			r.unregisterMailbox(MailboxID(msg.MailboxID))

		case connectionStatus:
			// HERE: Handling this and the errors in mailbox.go
			r.handleConnectionStatus(msg)

		case *internal.AllNodeClaims:
			r.handleAllNodeClaims(msg)

		case internal.AllNodeClaims:
			r.handleAllNodeClaims(&msg)

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
	_ = r.send(synchronizeRegistry{synch})
	<-synch
}

// generateAllNodeClaims returns a populated AllNodeClaims object suitable
// for synchronizing mailbox claims with a remote node.
func (r *registry) generateAllNodeClaims() *internal.AllNodeClaims {
	r.mu.RLock()
	defer r.mu.RUnlock()

	anc := internal.AllNodeClaims{
		Node:   internal.IntNodeID(r.connectionServer.nodeID),
		Claims: make(map[string]map[internal.IntMailboxID]struct{}),
	}

	for name, mailboxIDs := range r.claims {
		if _, ok := anc.Claims[name]; !ok {
			anc.Claims[name] = make(map[internal.IntMailboxID]struct{})
		}

		for intMailbox := range mailboxIDs {
			if intMailbox.NodeID() == r.thisNode {
				anc.Claims[name][internal.IntMailboxID(intMailbox)] = struct{}{}
			}
		}

		if len(anc.Claims[name]) == 0 {
			delete(anc.Claims, name)
		}
	}

	r.Tracef("Sending the following claims: %#v", anc)

	return &anc
}

func (r *registry) handleAllNodeClaims(msg *internal.AllNodeClaims) {
	re := registryEntries{}

	for name, mailboxIDs := range msg.Claims {
		for intMailboxID := range mailboxIDs {
			// Sanity check.
			if intMailboxID.NodeID() != msg.Node {
				r.Warnf("Omitting mailbox ID %x from registry sync because it's not local to node %d", intMailboxID, msg.Node)

				continue
			}

			re = append(
				re,
				registryEntry{
					name:      name,
					mailboxID: MailboxID(intMailboxID),
				},
			)
		}
	}

	r.registerAll(re)
}

// handleConnectionStatus deals with the connection to a node going up or
// down.
//
// It turns out we don't really care when a node comes up; we expect the
// semantics of the clustering to just carry us through until the node is
// up. But when the connection goes down, we do need to unregister all
// the claims on that node.
func (r *registry) handleConnectionStatus(msg connectionStatus) {
	r.Tracef("Handling connection status change: %#v", msg)

	if msg.connected {
		return
	}

	// Unregister all of the remote node registry entries.
	entries := registryEntries{}

	r.mu.RLock()
	for name, mailboxIDs := range r.claims {
		for mailboxID := range mailboxIDs {
			if mailboxID.NodeID() == msg.node {
				entries = append(entries, registryEntry{
					name:      name,
					mailboxID: mailboxID,
				})
			}
		}
	}
	r.mu.RUnlock()

	r.regMu.Lock()
	delete(r.nodeRegistries, msg.node)
	r.regMu.Unlock()

	if len(entries) > 0 {
		r.unregisterAll(entries)
	}
}

func (r *registry) toOtherNodes(msg interface{}) {
	r.regMu.RLock()
	defer r.regMu.RUnlock()

	for n, addr := range r.nodeRegistries {
		r.Tracef("Sending message to node %d: %#v", n, msg)
		_ = addr.Send(msg)
	}
}

func (r *registry) GetDebugger() NamesDebugger {
	return r
}

// Lookup returns an Address that can be used to send messages to a mailbox
// registered with the given string.
func (r *registry) Lookup(s string) (a *Address) {
	defer func() { r.Tracef("Lookup for %q returned Address %q", s, a) }()

	addresses := r.LookupAll(s)

	switch l := len(addresses); l {
	case 0:
	case 1:
		a = addresses[0]
	default:
		// Pick a random Address from our list of Addresses registered to this
		// name and return it.
		a = addresses[rand.Intn(l)]
	}

	return a
}

// LookupAll returns a slice of Addresses that can be used to send messages
// to the mailboxes registered with the given string.
func (r *registry) LookupAll(s string) []*Address {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var a []*Address

	claims, ok := r.claims[s]
	if !ok {
		return a
	}

	if len(claims) > 0 {
		cs := r.connectionServer

		for k := range claims {
			a = append(a, &Address{
				mailboxID:        k,
				connectionServer: cs,
				mailbox:          nil,
			})
		}
	}

	return a
}

func (r *registry) MultipleClaimCount() int32 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return atomic.LoadInt32(&r.multipleClaimCount)
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

	r.Tracef("Registering %q with address %x on this node", name, addr.mailboxID)

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
	if !addr.canBeGloballyUnregistered() {
		return
	}

	r.Tracef("Unregistering %q with address %x on this node", name, addr.mailboxID)

	// at the moment, only mailboxID can pass the check above
	_ = r.Send(internal.UnregisterName{
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
		r.Tracef("Unregistering mailbox %x on this node", mID)
		_ = r.Send(internal.UnregisterMailbox{
			Node:      internal.IntNodeID(node),
			MailboxID: internal.IntMailboxID(mID),
		})
	}
}

// register is the internal registration function.
func (r *registry) register(name string, mID MailboxID) {
	r.registerAll(
		registryEntries{
			registryEntry{
				name:      name,
				mailboxID: mID,
			},
		},
	)
}

// registerAll atomically registers registry entries.
func (r *registry) registerAll(entries registryEntries) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range entries {
		r.mailboxIDs[e.mailboxID] = append(r.mailboxIDs[e.mailboxID], e)
		mailboxIDs, haveMailboxIDs := r.claims[e.name]
		if !haveMailboxIDs {
			mailboxIDs = make(map[MailboxID]voidtype)
			r.claims[e.name] = mailboxIDs
		}

		preCount := len(mailboxIDs)
		mailboxIDs[e.mailboxID] = void
		postCount := len(mailboxIDs)

		// If there are multiple mailboxes (claimants) now and one of them is
		// local, notify all Addresses of the conflict.
		if postCount > 1 && e.mailboxID.NodeID() == r.thisNode {
			for mailboxID := range mailboxIDs {
				r.Tracef("Sending MultipleClaim for %q to Address %x", e.name, mailboxID)
				addr := &Address{
					mailboxID:        mailboxID,
					connectionServer: r.connectionServer,
				}

				err := addr.Send(MultipleClaim{Name: e.name})

				// Attempt to send the message to the mailbox.  If the mailbox is terminated,
				// unregister it.  This scenario can happen when a mailbox is registered and
				// terminated but never properly unregistered.  The MultipleClaim message will
				// never reach its destination and the caller of Register() will never know
				// about the multiple claim.
				if err == ErrMailboxTerminated {
					r.Tracef("Unregistering mailbox %x due to Mailbox Terminated error", mailboxID)
					r.Unregister(e.name, addr)
					continue
				}

				if err != nil {
					// Report on this.  This scenario could lead to persistent multiple claims.
					r.Warnf("Error sending MultipleClaim to %q: %s", e.name, err)
				}
			}
		}

		if preCount <= 1 && postCount > 1 {
			// Multiple claim for the current name.
			atomic.AddInt32(&r.multipleClaimCount, 1)
		}
	}
}

// unregister is the internal unregistration function. In the event that
// this results in the last registry for a given name being removed, we
// check for anyone currently waiting for a termination notice on that
// name and send it.
func (r *registry) unregister(name string, mID MailboxID) {
	r.unregisterAll(
		registryEntries{
			registryEntry{
				name:      name,
				mailboxID: mID,
			},
		},
	)
}

// unregisterAll atomically unregisters registry entries.
func (r *registry) unregisterAll(entries registryEntries) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range entries {
		mailboxIDs, ok := r.claims[e.name]
		if !ok {
			continue
		}

		preCount := len(mailboxIDs)
		delete(mailboxIDs, e.mailboxID)
		postCount := len(mailboxIDs)

		if preCount > 1 && postCount <= 1 {
			// No longer have a multiple claim for the current name.
			atomic.AddInt32(&r.multipleClaimCount, -1)
		}

		if postCount == 0 {
			delete(r.claims, e.name)
		}
	}
}

// unregisterMailbox unregisters all names associated with the given mailbox ID
func (r *registry) unregisterMailbox(mID MailboxID) {
	r.mu.Lock()
	entries, ok := r.mailboxIDs[mID]
	if ok {
		delete(r.mailboxIDs, mID)
	}
	r.mu.Unlock()

	if len(entries) > 0 {
		// Remove the entries from this node.
		r.unregisterAll(entries)

		// Broadcast out the unregistrations to the other nodes.
		for _, entry := range entries {
			r.toOtherNodes(internal.UnregisterName{
				Node:      internal.IntNodeID(r.thisNode),
				Name:      entry.name,
				MailboxID: internal.IntMailboxID(entry.mailboxID),
			})
		}
	}
}

// RegistryDebugger methods

func (r *registry) DumpClaims() map[string][]MailboxID {
	r.mu.RLock()
	defer r.mu.RUnlock()

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
	r.mu.RLock()
	defer r.mu.RUnlock()

	var count uint

	for _, addresses := range r.claims {
		count += uint(len(addresses))
	}

	return count
}

func (r *registry) AllNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.claims))
	for name := range r.claims {
		names = append(names, name)
	}
	return names
}

// SeenNames returns an array where each element corresponds to whether the inputted name
// at that index was found in the registry or not
func (r *registry) SeenNames(names ...string) []bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make([]bool, 0, len(names))
	for _, name := range names {
		_, ok := r.claims[name]
		seen = append(seen, ok)
	}
	return seen
}

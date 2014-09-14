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
	"errors"
	"fmt"

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

type registry struct {
	// the set of all claims understood by the local node, organized as
	// name -> set of mailbox IDs.
	claims map[string]map[AddressID]voidtype

	// claims arranged by node. this is necessary for processing when a
	// node connection goes down.
	nodeClaims map[NodeID]map[string]mailboxID

	// The map of nodes -> addresses used to communicate with those nodes.
	// When a registration is made, this is the list of nodes that will
	// receive notifications of the new address. This only has remote nodes.
	nodeRegistries map[NodeID]Address

	// This is the set of notification on terminate we currently have.
	// When a node name is unclaimed by anybody, we check this for any
	// notifications that need to go out.
	notifications map[registryMailbox]map[AddressID]voidtype

	Address
	*Mailbox

	thisNode NodeID
}

type connectionStatus struct {
	node      NodeID
	connected bool
}

// A registryMailbox is a mailbox that reflects the Registry's current
// state... that is, at the time you "send" a message, it uses the
// registry's current information about that name to determine what to
// do. It is, therefore, dynamic.
type registryMailbox string

type sendRegistryMessage struct {
	mailbox registryMailbox
	message interface{}
}

type notifyOnTerminateRegistryAddr struct {
	mailbox registryMailbox
	addr    Address
}

type removeNotifyOnTerminateRegistryAddr struct {
	mailbox registryMailbox
	addr    Address
}

// conceptually, I consider this inline with the newConnections function,
// since this deeply depends on the order of parameters initialized in the
// &connectionServer{} in an otherwise icky way...
func newRegistry(server *connectionServer, node NodeID) *registry {
	r := &registry{
		claims:     make(map[string]map[AddressID]voidtype),
		nodeClaims: make(map[NodeID]map[string]mailboxID),
		thisNode:   node,
	}

	for node := range server.nodeConnectors {
		r.nodeClaims[node] = make(map[string]mailboxID)
	}

	r.Address, r.Mailbox = server.mailboxes.newLocalMailbox()

	server.AddConnectionStatusCallback(r.connectionStatusCallback)

	return r
}

func (r *registry) Terminate() {
	r.Mailbox.Terminate()
}

func (r *registry) connectionStatusCallback(node NodeID, connected bool) {
	r.Send(connectionStatus{node, connected})
}

func (r *registry) Stop() {
	r.Send(stopRegistry{})
}

func (r *registry) Serve() {
	for {
		m := r.ReceiveNext()
		switch msg := m.(type) {
		case sendRegistryMessage:
			fmt.Println(msg)

		case notifyOnTerminateRegistryAddr:
			names, haveNames := r.claims[string(msg.mailbox)]
			if !haveNames || len(names) == 0 {
				msg.addr.Send(MailboxTerminated(msg.mailbox))
			}
			notificationsForName, haveNotifications := r.notifications[msg.mailbox]
			if haveNotifications {
				notificationsForName[msg.addr.GetID()] = void
			} else {
				r.notifications[msg.mailbox] = map[AddressID]voidtype{msg.addr.GetID(): void}
			}

		case removeNotifyOnTerminateRegistryAddr:
			names, haveNames := r.claims[string(msg.mailbox)]
			if !haveNames {
				return
			}

			delete(names, msg.mailbox)

		case internal.RegisterName:
			r.register(NodeID(msg.Node), msg.Name, mailboxID(msg.AddressID))

			if NodeID(msg.Node) == r.thisNode {
				r.toOtherNodes(msg)
			}

		case internal.UnregisterName:
			r.unregister(NodeID(msg.Node), msg.Name, mailboxID(msg.AddressID))

			if NodeID(msg.Node) == r.thisNode {
				r.toOtherNodes(msg)
			}

		case connectionStatus:
			// HERE: Handling this and the errors in mailbox.go
			r.handleConnectionStatus(msg)

		case internal.AllNodeClaims:
			r.handleAllNodeClaims(msg)

		case stopRegistry:
			return

		case MailboxTerminated:
			return
		}
	}
}

func (r *registry) handleAllNodeClaims(msg internal.AllNodeClaims) {
	node := NodeID(msg.Node)
	for name, intMailbox := range msg.Claims {
		r.register(node, name, mailboxID(intMailbox))
	}
}

// This registers a node's registry mailbox. This should only happen once
// per connection to that node. Even if it is the same as last time, it
// triggers the synchronization process.
func (r *registry) registerNodeRegistryMailbox(node NodeID, addr Address) {
	r.nodeRegistries[node] = addr

	// send initial synchronization
	claims := map[string]internal.IntMailboxID{}
	anc := internal.AllNodeClaims{internal.IntNodeID(r.thisNode), claims}

	// Copy the claims. This has to be a fresh copy to make sure we don't
	// lose anything to race conditions. If this becomes a performance
	// bottleneck, we could try to do a serialization immediately in this
	// step, since this gets copied and then serialized anyhow.
	for name, mID := range r.nodeClaims[r.thisNode] {
		claims[name] = internal.IntMailboxID(mID)
	}

	addr.Send(anc)
}

// Handle connection status deals with the connection to a node going up or
// down.
//
// It turns out we don't really care when a node comes up; we expect the
// semantics of the clustering to just carry us through until the node is
// up (all attempts to link to any remote addresses we may have will get
// MailboxTerminated back from the clustering anyhow). But when the
// connection goes down, we do need to unregister all the claims on that
// node, which will then trigger the usual handling of MailboxTerminated
// messages.
func (r *registry) handleConnectionStatus(msg connectionStatus) {
	if msg.connected == true {
		return
	}

	for name, claimant := range r.nodeClaims[msg.node] {
		r.unregister(msg.node, name, claimant)
	}

	delete(r.nodeRegistries, msg.node)
}

func (r *registry) toOtherNodes(msg interface{}) {
	for _, addr := range r.nodeRegistries {
		addr.Send(msg)
	}
}

// Lookup returns an Address that can be used to send to the mailboxes
// registered with the given string.
//
func (r *registry) Lookup(s string) Address {
	return Address{registryMailbox(s), nil, nil}
}

// Register claims the given global name in the registry.
//
// This does not happen synchronously, as there seems to be no reason
// for the caller to synchronously wait for this.
//
// A registered mailbox should stand ready to receive MultipleClaim
// messages from the cluster.
func (r *registry) Register(name string, addr Address) error {
	if !addr.GetID().canBeGloballyRegistered() {
		return ErrCantGloballyRegister
	}

	// only mailboxID can pass the canBeGloballyRegistered test above
	r.Send(internal.RegisterName{
		internal.IntNodeID(r.thisNode),
		name,
		internal.IntMailboxID(addr.GetID().(mailboxID)),
	})
	return nil
}

// Unregister removes the given claim from a given global name.
// Unregistration will only occur if the current registrant matches the
// address passed in. It is not an error for it not to match; the call will
// simply be ignored.
//
// On a given node, only one Address can have a claim on a
// name. If you wish to supercede a claim with a new address, you can
// simply register the new claim, and it will overwrite the previous one.
func (r *registry) Unregister(name string, addr Address) {
	if !addr.GetID().canBeGloballyRegistered() {
		return
	}

	// at the moment, only mailboxID can pass the check above
	r.Send(internal.UnregisterName{
		internal.IntNodeID(r.thisNode),
		name,
		internal.IntMailboxID(addr.GetID().(mailboxID)),
	})
}

// This is the internal registration function.
func (r *registry) register(node NodeID, name string, mID mailboxID) {
	oldMailboxID, haveOld := r.nodeClaims[node][name]
	if haveOld {
		delete(r.claims[name], oldMailboxID)
	}

	nameClaimants, haveNameClaimants := r.claims[name]
	if !haveNameClaimants {
		nameClaimants = map[AddressID]voidtype{}
		r.claims[name] = nameClaimants
	}

	nameClaimants[mID] = void

	// If there are multiple claims now, and one of them is local,
	// notify our local Address of the conflict.
	localClaimant, haveLocalClaimant := r.nodeClaims[r.thisNode][name]
	if len(nameClaimants) > 1 && haveLocalClaimant {
		claimants := []Address{}
		for claimant := range nameClaimants {
			var addr Address
			addr.id = claimant
			claimants = append(claimants, addr)
		}
		var localClaimantAddr Address
		// only possible error from unmarshalFromID is wrong type,
		// which we prevent by construction
		localClaimantAddr.id = localClaimant
		localClaimantAddr.Send(MultipleClaim{claimants, name})
	}
}

// this is the internal unregistration function. In the event that this
// results in the last registry for a given name being removed, we check
// for anyone currently waiting for a termination notice on that name
// and send it.
func (r *registry) unregister(node NodeID, name string, mID mailboxID) {
	// ensure that the unregistration matches the current one before
	// removing it. zero value for mailboxID will never match a real one
	// by construction in newMailboxes() (where nextMailboxID starts at 1).
	currentRegistrant := r.nodeClaims[node][name]
	if currentRegistrant != mID {
		return
	}

	delete(r.nodeClaims[node], name)

	// paranoia, this really shouldn't happen, but if it did, it would be bad
	_, inGlobalRegistration := r.claims[name][mID]
	if !inGlobalRegistration {
		panic("Serious bug: Somehow unregistered something not globally registered")
	}

	delete(r.claims[name], mID)

	if len(r.claims[name]) == 0 {
		rm := registryMailbox(name)
		for mailboxToNotify := range r.notifications[rm] {
			var addr Address
			addr.id = mailboxToNotify
			addr.Send(MailboxTerminated(rm))
		}
		delete(r.notifications, rm)
	}
}

func (rm registryMailbox) isLocal() bool {
	return false
}

func (rm registryMailbox) send(msg interface{}) error {
	connections.registry.Send(sendRegistryMessage{rm, msg})
	return nil
}

func (rm registryMailbox) getID() AddressID {
	return rm
}

func (rm registryMailbox) notifyAddressOnTerminate(addr Address) {
	connections.registry.Send(notifyOnTerminateRegistryAddr{rm, addr})
}

func (rm registryMailbox) removeNotifyAddress(addr Address) {
	connections.registry.Send(removeNotifyOnTerminateRegistryAddr{rm, addr})
}

func (rm registryMailbox) canBeGloballyRegistered() bool {
	return false
}

// Register globally registers a given name across the cluster to the given
// address. It can then be retrieved and manipulated via Lookup.
//
// The passed-in Address must be something that directly came from a New()
// call. Addressed obtained from the network or from Lookup itself will
// return an error instead of registering.
func Register(name string, addr Address) error {
	return connections.registry.Register(name, addr)
}

// Unregister globally unregisters a given name across the cluster.
//
// If the address passed in is not the current registrant, the call is
// ignored, thus it is safe to call this.
func Unregister(name string, addr Address) {
	connections.registry.Unregister(name, addr)
}

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
func Lookup(name string) Address {
	return Address{registryMailbox(name), nil, nil}
}

package reign

// TODO: Implement a timer for ReceiveTimed, which will take a duration and
// use a Timer.AfterFunc to send the time out message, which will be
// processed into a return value afterwards.

// FIXME: Add the timestamp into the PIDs, so that cluster nodes can tell
// whether or not the PIDs belong to the current run. Erlang seems to do
// something like this. See if I can somehow get away with just the
// clusters generating some sort of timestamp... not sure if I'll be that lucky.

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"reign/internal"

	"strconv"
	"sync"
	"sync/atomic"
)

const (
	// 2 ^ 56 - 1
	maxMailboxID = 72057594037927935
)

func init() {
	var addr Address
	gob.Register(&addr)
}

// ErrIllegalAddressFormat is returned when something attempts to
// unmarshal an illegal text or binary string into an Address.
var ErrIllegalAddressFormat = errors.New("illegally-formatted address")

var errIllegalNilSlice = errors.New("can't unmarshal nil slice into an address")

// An AddressID is an opaque identifier that can be safely used as a
// key in a map. It can also be used to obtain an address from the
// Mailboxes object. It should not be further examined in user code.
type AddressID interface {
	// internally, literal Address struct values don't really have enough
	// existence to justify an "ID", what we really care about are Mailbox
	// IDs. However, what the library user can do with an ID is construct
	// an Address, not construct a Mailbox, so we give this the public name
	// of AddressID. Internally you'll still see "mailboxID" used in
	// various guises in a lot of places, because that's what it really is.

	// isLocal returns true if this is a strictly local mailbox.
	// if it's a registryMailbox this is always assumed false.
	// FIXME: Probably defunct now.
	isLocal() bool

	// This is true if the address can legally be globally registered.
	// Only local addresses can be registered.
	canBeGloballyRegistered() bool
}

// See comment in AddressID above.
type mailboxID uint64

func (mID mailboxID) NodeID() NodeID {
	return NodeID(uint64(mID) & 255)
}

func (mID mailboxID) mailboxOnlyID() uint64 {
	return uint64(mID >> 8)
}

func (mID mailboxID) getID() AddressID {
	return mID
}

func (mID mailboxID) isLocal() bool {
	nodeID := mID.NodeID()
	return nodeID == connections.ThisNode.ID
}

func (mID mailboxID) canBeGloballyRegistered() bool {
	return true
}

// MailboxTerminated is sent to Addresses that request notification
// of when a Mailbox is being terminated, with NotifyAddressOnTerminate.
// If you request termination notification of multiple mailboxes, this can
// be converted to an AddressID which can be used to distinguish them.
type MailboxTerminated AddressID

type mailboxes struct {
	nextMailboxID mailboxID
	nodeID        NodeID

	// this isn't an ideal data structure. It's enough to satisfy the author's
	// use case, but if you throw "enough" cores at this and create mailboxes
	// rapidly enough, this could start to become a bottleneck.
	// Still, this *is* only touched at creation and deletion of mailboxes,
	// not on every message or anything.
	mailboxes map[mailboxID]*Mailbox

	connectionServer *connectionServer
	sync.RWMutex
}

// Returns a new set of mailboxes. This is used by the clustering
// code. Users would not normally call this.
func newMailboxes(connectionServer *connectionServer, nodeID NodeID) *mailboxes {
	return &mailboxes{
		nextMailboxID:    1,
		nodeID:           nodeID,
		connectionServer: connectionServer,
		mailboxes:        make(map[mailboxID]*Mailbox),
	}
}

// This error indicates that the target mailbox has (already) been terminated.
var ErrMailboxTerminated = errors.New("mailbox has been terminated")

// This error indicates that you passed a remote mailbox's MailboxID
// to a function that only works on local mailboxes.
var ErrNotLocalMailbox = errors.New("function required a local mailbox ID but this is a remote MailboxID")

// An Address is the public face of the Mailbox. It is fine to pass this
// by value.
//
// WARNING: It is not safe to use either Address or *Address for equality
// testing or as a key in maps! Use .GetID() to obtain a AddressID, which
// is. (Both Address and *Address are fine to store as values.)
type Address struct {
	id AddressID

	// This is usually left nil, and picked up off the global value.
	// However, in order to permit testing that simulates multiple nodes
	// being run in one process, this can be set after an unmarshal to
	// force an Address to use a particular connection server.
	connectionServer *connectionServer

	// If this is a local mailbox and we can resolve it as such, we cache
	// it here.
	mailbox address
}

// GetID returns the AddressID associated with this Address.
//
// This is safe to use as keys for maps.
func (a Address) GetID() AddressID {
	return a.id
}

func (a *Address) clearAddress() {
	a.id = nil
	a.connectionServer = nil
	a.mailbox = nil
}

var rmIsAddress = registryMailbox("")

func (a *Address) getAddress() address {
	if a.mailbox != nil {
		return a.mailbox
	}

	// registryMailbox is an address and a MailboxID already:
	if addr, isAddress := a.id.(address); isAddress {
		return addr
	}

	if a.id == nil {
		return nil
	}

	// As of this writing, the only two AddressIDs are registryMailbox,
	// caught in the above check, and mailboxIDs.
	mailboxID := a.id.(mailboxID)

	nodeID := mailboxID.NodeID()

	var c *connectionServer
	if a.connectionServer != nil {
		c = a.connectionServer
	} else {
		c = connections
	}

	// If this is a local mailbox, try to go get the local address. We have
	// to do this, because while we can easily forge up an "Address" with
	// the same ID, we have to get the one actually attached to a Mailbox
	// to work.
	if nodeID == c.ThisNode.ID {
		mbox, err := c.mailboxes.mailboxByID(mailboxID)
		if err == nil {
			a.mailbox = mbox
			return mbox
		}

		// since the above if clause forces addressByID down the
		// same if branch in its implementation, the only possible
		// error is ErrMailboxTerminated. While this is in some
		// sense an error for the user, as far as marshaling is
		// concerned, this is success, because it's perfectly legal to
		// unmarshal an address corresponding to something that has
		// since terminated, just as it's perfectly legal to hold on to
		// a reference to a mailbox that has terminated. We do however
		// short-circuit everything else about the mailbox by returning
		// this "noMailbox" shim.
		a.mailbox = noMailbox{mailboxID}
		return a.mailbox
	}

	remoteMailboxes, exists := c.remoteMailboxes[mailboxID.NodeID()]
	if !exists {
		panic("Somehow trying to unmarshal a mailbox on an undefined node")
	}

	a.mailbox = boundRemoteAddress{mailboxID, remoteMailboxes}
	return a.mailbox
}

// RegisterType registers a type to be sent across the cluster.
//
// This wraps gob.Register, in case we ever change the encoding method.
func RegisterType(value interface{}) {
	gob.Register(value)
}

// Send something to the target mailbox.
//
// All concrete types that you wish to send across the cluster must
// have .Register called on them. See the documentation on gob.Register
// for the reason why. (The local .RegisterType abstracts our dependency on
// gob. If you don't register through reign's .RegisterType, future versions
// of this package may require you to fix that.)
//
// The error is primarily for internal purposes. If the mailbox is
// local, and has been terminated, ErrMailboxTerminated will be
// returned.
//
// An error guarantees failure, but lack of error does not guarantee
// success! Arguably, "ErrMailboxTerminated" should be seen as a purely
// internal detail, and just like in Erlang, if you want a guarantee
// you must implement an acknowledgement. However, just like in
// Erlang, we leak this internal detail a bit. I don't know if
// that's a good idea; use with caution. (See: erlang:is_process_alive,
// which similarly leaks out whether the process is local or not.)
func (a Address) Send(m interface{}) error {
	return a.getAddress().send(m)
}

// NotifyAddressOnTerminate requests that the target address receive a
// termination notice when the target address is terminated.
//
// This is like linking in Erlang, and is intended to provide the
// same guarantees.
//
// While addresses and goroutines are not technically bound together,
// it is convenient to think of an address "belonging" to a goroutine.
// From that point of view, note the caller is the *argument*, not
// the object. Calls look like:
//
//     otherAddress.NotifyAddressOnTerminate(myAddress)
//
// which translates in English as "Other address, please let me
// know when you are terminated."
//
// Calling this more than once with the same address may or may not
// cause multiple notifications to occur.
func (a Address) NotifyAddressOnTerminate(addr Address) {
	a.getAddress().notifyAddressOnTerminate(addr)
}

// RemoveNotifyAddress will remove the notification request from the
// Address you call this on.
//
// This does not guarantee that you will not receive a termination
// notification from the Address, due to race conditions.
func (a Address) RemoveNotifyAddress(addr Address) {
	a.getAddress().removeNotifyAddress(addr)
}

// MarshalBinary implements binary marshalling for Addresses.
//
// A marshalled Address only carries its identifier. When unmarshalled on
// the same node, the unmarshalled address will be reconnected to the
// original Mailbox. If unmarshalled on a different node, a reference to
// the remote mailbox will be unmarshaled.
func (a Address) MarshalBinary() ([]byte, error) {
	address := a.getAddress()

	if address == nil {
		return nil, ErrIllegalAddressFormat
	}

	switch mbox := address.(type) {
	case *Mailbox:
		b := make([]byte, 10, 10)
		written := binary.PutUvarint(b, uint64(mbox.id))
		return append([]byte("<"), b[:written]...), nil

	case noMailbox:
		return []byte("X"), nil

	case boundRemoteAddress:
		b := make([]byte, 10, 10)
		written := binary.PutUvarint(b, uint64(mbox.mailboxID))
		return append([]byte("<"), b[:written]...), nil

	case registryMailbox:
		return []byte("\"" + string(mbox)), nil

	default:
		return nil, ErrIllegalAddressFormat
	}

}

// UnmarshalBinary implements binary unmarshalling for Addresses.
func (a *Address) UnmarshalBinary(b []byte) error {
	a.clearAddress()

	if len(b) == 0 {
		return ErrIllegalAddressFormat
	}

	if b[0] == 60 { // this is "<"
		id, readBytes := binary.Uvarint(b[1:])
		if readBytes == 0 {
			return ErrIllegalAddressFormat
		}
		a.id = mailboxID(id)
		return nil
	}

	if b[0] == 34 { // double-quote
		rm := registryMailbox(string(b[1:]))
		a.id = rm
		a.mailbox = rm
		return nil
	}

	if len(b) == 1 && b[0] == 88 { // capital X
		a.mailbox = noMailbox{0}
		a.id = mailboxID(0)
		return nil
	}

	return errors.New("illegal value passed to Address.UnmarshalBinary")
}

// UnmarshalFromID allows you to obtain a legal address from an AddressID.
// Use as:
//
//    var addr reign.Address
//    err := addr.UnmarshalFromID(addressID)
//    if err == nil {
//        addr.Send(...)
//    }
func (a *Address) UnmarshalFromID(addrID AddressID) {
	a.clearAddress()

	a.id = addrID
	a.mailbox = nil
}

// UnmarshalText implements text unmarshalling for Addresses.
func (a *Address) UnmarshalText(b []byte) error {
	a.clearAddress()

	if b == nil {
		return errIllegalNilSlice
	}

	// must be a mailboxID of one sort or another
	switch b[0] {
	case byte('<'):
		// Longest possible text address: A full 3 bytes for the cluster,
		// a full 16 bytes for the mailboxID, and three more bytes for the
		// <:>
		if len(b) > 23 {
			return ErrIllegalAddressFormat
		}

		if b[len(b)-1] != byte('>') {
			return ErrIllegalAddressFormat
		}

		b = b[1 : len(b)-1]
		ids := bytes.Split(b, []byte(":"))
		if len(ids) != 2 {
			return ErrIllegalAddressFormat
		}
		nodeID, err := strconv.ParseUint(string(ids[0]), 10, 8)
		if err != nil {
			return err
		}
		mailboxIDNum, err := strconv.ParseUint(string(ids[1]), 10, 64)
		if err != nil {
			return err
		}
		if mailboxIDNum > maxMailboxID {
			return ErrIllegalAddressFormat
		}

		a.id = mailboxID(mailboxIDNum<<8 + nodeID)

		return nil

	case byte('X'):
		a.mailbox = noMailbox{0}
		a.id = mailboxID(0)
		return nil

	case byte('"'):
		if b[len(b)-1] != byte('"') {
			return ErrIllegalAddressFormat
		}
		rm := registryMailbox(b[1 : len(b)-1])
		a.mailbox = rm
		a.id = rm
		return nil
	}
	return ErrIllegalAddressFormat
}

// MarshalText implements text marshalling for Addresses.
//
// See MarshalBinary.
func (a Address) MarshalText() ([]byte, error) {
	switch mbox := a.mailbox.(type) {
	case *Mailbox:
		ClusterID := mbox.id.NodeID()
		mailboxID := mbox.id.mailboxOnlyID()
		text := fmt.Sprintf("<%d:%d>", ClusterID, mailboxID)
		return []byte(text), nil

	case noMailbox:
		return []byte("X"), nil

	case boundRemoteAddress:
		ClusterID := mbox.mailboxID.NodeID()
		mailboxID := mbox.mailboxID.mailboxOnlyID()
		text := fmt.Sprintf("<%d:%d>", ClusterID, mailboxID)
		return []byte(text), nil

	case registryMailbox:
		return []byte(fmt.Sprintf("\"%s\"", string(mbox))), nil

	default:
		return nil, errors.New("unknown address type, internal reign error")
	}
}

func (a Address) String() string {
	b, _ := a.MarshalText()
	return string(b)
}

type address interface {
	send(interface{}) error

	getID() AddressID

	notifyAddressOnTerminate(Address)
	removeNotifyAddress(Address)
}

// This type is returned when unmarshalling a local address that doesn't
// exist.
//
// Note that if you unmarshal an address that doesn't exist *yet*, you
// still get this and it will never change. To do that implies you
// unmarshalled something that didn't come from a marshal OR that
// you unmarshalled something from a previous execution, and either way
// this should not "magically" turn into a mailbox at some point in the
// future.
type noMailbox struct {
	mailboxID
}

func (nm noMailbox) send(interface{}) error {
	return ErrMailboxTerminated
}

func (nm noMailbox) getID() AddressID {
	return nm.mailboxID
}

func (nm noMailbox) notifyAddressOnTerminate(target Address) {
	target.Send(MailboxTerminated(nm.mailboxID))
}

func (nm noMailbox) removeNotifyAddress(target Address) {}

func (nm noMailbox) canBeGloballyRegistered() bool {
	return false
}

// A boundRemoteAddress is only used for testing in the "multinode"
// configuration. This allows us to switch into the correct node context
// when sending messages to the target mailbox, which allows us to ensure
// that we are correctly simulating the network send.
//
// FIXME: Too much indirection here, there's no reason not to bind this
// directly to the target address.
type boundRemoteAddress struct {
	mailboxID
	*remoteMailboxes
}

func (bra boundRemoteAddress) send(message interface{}) error {
	// FIMXE: Have to pass along the mailboxID here.
	return bra.remoteMailboxes.Send(internal.OutgoingMailboxMessage{
		Target:  internal.IntMailboxID(bra.mailboxID),
		Message: message,
	})
}

func (bra boundRemoteAddress) notifyAddressOnTerminate(addr Address) {
	// as this is internal only, we can just hard-assert the local address
	// is a "real" mailbox
	bra.remoteMailboxes.Send(internal.NotifyRemote{
		Remote: internal.IntMailboxID(bra.mailboxID),
		Local:  internal.IntMailboxID(addr.GetID().(mailboxID)),
	})
}

func (bra boundRemoteAddress) removeNotifyAddress(addr Address) {
	// as this is internal only, we can just hard-assert the local address
	// is a "real" mailbox
	bra.remoteMailboxes.Send(internal.UnnotifyRemote{
		Remote: internal.IntMailboxID(bra.mailboxID),
		Local:  internal.IntMailboxID(addr.GetID().(mailboxID)),
	})
}

func (bra boundRemoteAddress) canBeGloballyRegistered() bool {
	return false
}

// A Mailbox is what you receive messages from via Receive or RecieveNext.
type Mailbox struct {
	id                    mailboxID
	messages              []message
	cond                  *sync.Cond
	notificationAddresses map[AddressID]struct{}
	terminated            bool
	// used only by testing, to implement the ability to block until
	// a notification has been processed
	broadcastOnAddNotify bool
	parent               *mailboxes
}

type message struct {
	msg interface{}
}

// New creates a new tied pair of Address and Mailbox.
//
// A Mailbox MUST have .Terminate() called on it when you are done with
// it. Otherwise termination notifications will not properly fire and
// resources will leak.
//
// It is not safe to copy a Mailbox by value; client code should never have
// Mailbox appearing as a non-pointer-type in its code. It is a code smell
// to have *Mailbox used as a map key; use AddressIDs instead.
// instead.
func New() (Address, *Mailbox) {
	c := connections
	if c == nil {
		panic("Can not create mailboxes before a cluster configuration has been chosen. (Consider calling reign.NoClustering.)")
	}
	return c.newLocalMailbox()
}

func (m *mailboxes) newLocalMailbox() (Address, *Mailbox) {
	var mutex sync.Mutex
	cond := sync.NewCond(&mutex)

	nextID := mailboxID(atomic.AddUint64((*uint64)(&m.nextMailboxID), 1))
	m.nextMailboxID += 1
	id := nextID<<8 + mailboxID(m.nodeID)

	mailbox := &Mailbox{
		id:       id,
		messages: make([]message, 0, 1),
		cond:     cond,
		notificationAddresses: nil,
		terminated:            false,
		parent:                m,
	}

	m.registerMailbox(id, mailbox)

	return Address{id, nil, mailbox}, mailbox
}

func (m *Mailbox) getID() AddressID {
	return m.id
}

func (m *Mailbox) send(msg interface{}) error {
	m.cond.L.Lock()
	if m.terminated {
		// note: can't just defer here, Signal must follow Unlock in the
		// other branch
		m.cond.L.Unlock()
		return ErrMailboxTerminated
	}

	message := message{msg}
	m.messages = append(m.messages, message)
	m.cond.L.Unlock()

	m.cond.Signal()

	return nil
}

func (m *Mailbox) notifyAddressOnTerminate(target Address) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if m.terminated {
		target.Send(MailboxTerminated(m.id))
		return
	}

	if m.notificationAddresses == nil {
		m.notificationAddresses = make(map[AddressID]struct{})
	}
	m.notificationAddresses[target.GetID()] = struct{}{}

	if m.broadcastOnAddNotify {
		m.cond.Broadcast()
	}
}

// This test function will block until the target address is in
// this localAddressImpl's notificationAddresses.
//
// Since this is a test-only function, we only implement enough to let
// one listen on a given address occur at a time.
func (m *Mailbox) blockUntilNotifyStatus(target Address, desired bool) {
	m.cond.L.Lock()

	m.broadcastOnAddNotify = true

	id := target.GetID()

	_, exists := m.notificationAddresses[id]
	for exists != desired {
		m.cond.Wait()
		_, exists = m.notificationAddresses[id]
	}
	m.broadcastOnAddNotify = false
	m.cond.L.Unlock()
}

func (m *Mailbox) removeNotifyAddress(target Address) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if m.notificationAddresses != nil {
		delete(m.notificationAddresses, target.GetID())
	}
}

// ReceiveNext will receive the next message sent to this mailbox.
// It blocks until the next message comes in, which may be forever.
// If the mailbox is terminated, it will receive a MailboxTerminated reply.
//
// If you've got multiple receivers on a single mailbox, be sure to check
// for MailboxTerminated.
func (m *Mailbox) ReceiveNext() interface{} {
	// FIXME: Verify three listeners on one shared mailbox all get
	// terminated properly.
	m.cond.L.Lock()
	for len(m.messages) == 0 && !m.terminated {
		m.cond.Wait()
	}

	if m.terminated {
		m.cond.L.Unlock()
		return MailboxTerminated(m.id)
	}

	msg := m.messages[0]
	// in the common case of not having a message backlog, this
	// should prevent a lot of garbage buildup by reusing the slot.
	if len(m.messages) == 1 {
		m.messages = m.messages[:0]
	} else {
		m.messages = m.messages[1:]
	}
	m.cond.L.Unlock()
	return msg.msg
}

// Receive will receive the next message sent to this mailbox that matches
// according to the passed-in function.
//
// Receive assumes that it is the only function running against the
// Mailbox. If you Receive from multiple goroutines, or Receive in one
// and ReceiveNext in another, you *will* miss messages in the routine
// calling Receive.
//
// I recommend that your matcher function be:
//
//  func (i) (ok bool) {
//      _, ok = i.(SomeType)
//      return
//  }
//
// If the mailbox gets terminated, this will return a MailboxTerminated,
// regardless of the behavior of the matcher.
func (m *Mailbox) Receive(matcher func(interface{}) bool) interface{} {
	m.cond.L.Lock()

	if m.terminated {
		m.cond.L.Unlock()
		return MailboxTerminated(m.id)
	}

	// see if there are any messages that match
	for i, v := range m.messages {
		if matcher(v.msg) {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			m.cond.L.Unlock()
			return v.msg
		}
	}

	// Loop until we get the message we want
	// remember, this assumes this function is the *only* way to receive a message,
	// so we can tell if another message came in just by looking at the
	// length of the queue.
	for {
		lastIdx := len(m.messages)

		for len(m.messages) == lastIdx && !m.terminated {
			m.cond.Wait()
		}

		if m.terminated {
			m.cond.L.Unlock()
			return MailboxTerminated(m.id)
		}

		for ; lastIdx < len(m.messages); lastIdx++ {
			if matcher(m.messages[lastIdx].msg) {
				match := m.messages[lastIdx].msg
				m.messages = append(m.messages[:lastIdx], m.messages[lastIdx+1:]...)
				m.cond.L.Unlock()
				return match
			}
		}
	}
}

// Terminate shuts down a given mailbox. Once terminated, a mailbox
// will reject messages without even looking at them, and can no longer
// have any Receive used on them.
//
// Further, it will notify any registered Addresses that it has been terminated.
//
// This facility is used analogously to Erlang's "link" functionality.
// Of course in Go you can't be notified when a goroutine terminates, but
// if you defer mailbox.Terminate() in the proper place for your mailbox
// user, you can get most of the way there.
//
// FIXME: What do we do with this upon network partition? What does Erlang do?
//
// It is not an error to Terminate an already-Terminated mailbox.
func (m *Mailbox) Terminate() {
	// I think doing this before locking our own lock is correct; we are
	// already uninterested in any future operations, and double-deleting
	// out of this dict is OK.
	m.parent.unregisterMailbox(m.id)

	m.cond.L.Lock()

	// this is not redundant; m.terminated is part of what we have to lock
	if m.terminated {
		m.cond.L.Unlock()
		return
	}

	m.terminated = true

	terminating := MailboxTerminated(m.id)
	connectionServer := m.parent.connectionServer
	for mailboxID := range m.notificationAddresses {
		var addr Address
		addr.id = mailboxID
		addr.connectionServer = connectionServer
		addr.Send(terminating)
	}

	// chuck out what garbage we can
	m.notificationAddresses = nil
	m.messages = nil

	m.cond.L.Unlock()
	m.cond.Broadcast()
}

// it is an error to call this with the same mID more than once
func (m *mailboxes) registerMailbox(mID mailboxID, mbox *Mailbox) {
	m.Lock()
	defer m.Unlock()

	m.mailboxes[mID] = mbox
}

func (m *mailboxes) unregisterMailbox(mID mailboxID) {
	m.Lock()
	defer m.Unlock()

	delete(m.mailboxes, mID)
}

func (m *mailboxes) sendByID(mID mailboxID, msg interface{}) error {
	mailbox, err := m.mailboxByID(mID)
	if err != nil {
		return err
	}
	return mailbox.send(msg)
}

func (m *mailboxes) mailboxByID(mID mailboxID) (mbox *Mailbox, err error) {
	if mID.NodeID() != m.nodeID {
		err = ErrNotLocalMailbox
		return
	}
	m.RLock()
	mbox, exists := m.mailboxes[mID]
	m.RUnlock()
	if exists {
		return
	}
	err = ErrMailboxTerminated
	return
}

func (m *mailboxes) mailboxCount() int {
	m.RLock()
	defer m.RUnlock()

	return len(m.mailboxes)
}

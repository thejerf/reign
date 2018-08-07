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
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thejerf/reign/internal"
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

// ErrMailboxTerminated is returned when the target mailbox has (already) been
// terminated.
var ErrMailboxTerminated = errors.New("mailbox has been terminated")

// ErrNotLocalMailbox is returned when a remote mailbox's MailboxID is passed
// into a function that only works on local mailboxes.
var ErrNotLocalMailbox = errors.New("function required a local mailbox ID but this is a remote MailboxID")

type address interface {
	send(interface{}) error

	notifyAddressOnTerminate(*Address)
	removeNotifyAddress(*Address)
	canBeGloballyRegistered() bool
	canBeGloballyUnregistered() bool
	getMailboxID() MailboxID
}

type message struct {
	msg interface{}
}

// An Address is the public face of the Mailbox. It is fine to pass this
// by value.
//
// WARNING: It is not safe to use either Address or *Address for equality
// testing or as a key in maps! Use .GetID() to obtain a AddressID, which
// is. (Both Address and *Address are fine to store as values.)
type Address struct {
	mailboxID MailboxID
	// mailbox is the cached Mailbox/boundRemoteAddress/noMailbox that we
	// might have previously received from calling Send() on this object.
	mailbox address

	// connectionServer is usually left nil, and picked up off the global value.
	// However, in order to permit testing that simulates multiple nodes
	// being run in one process, this can be set after an unmarshal to
	// force an Address to use a particular connection server.
	connectionServer *connectionServer
}

// GetID returns the MailboxID of the Address
func (a *Address) GetID() MailboxID {
	return a.mailboxID
}

func (a *Address) canBeGloballyRegistered() bool {
	// If no mailbox is cached, resolve it now
	if a.mailbox == nil {
		a.getAddress()
	}

	return a.mailbox.canBeGloballyRegistered()
}

func (a *Address) canBeGloballyUnregistered() bool {
	// If no mailbox is cached, resolve it now
	if a.mailbox == nil {
		a.getAddress()
	}

	return a.mailbox.canBeGloballyUnregistered()
}

func (a *Address) getAddress() address {
	// We've already got a cached Mailbox; use it
	if a.mailbox != nil {
		return a.mailbox
	}

	nodeID := a.mailboxID.NodeID()
	mailboxID := a.mailboxID

	if a.connectionServer == nil {
		a.connectionServer = connections
	}

	c := a.connectionServer

	if c == nil {
		panic("connection server is nil")
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
		a.mailbox = &noMailbox{a.mailboxID}
		return a.mailbox
	}

	remoteMailboxes, exists := c.remoteMailboxes[nodeID]
	if !exists {
		panic("Somehow trying to unmarshal a mailbox on an undefined node")
	}

	a.mailbox = boundRemoteAddress{mailboxID, remoteMailboxes}
	return a.mailbox
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
func (a *Address) Send(m interface{}) error {
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
func (a *Address) NotifyAddressOnTerminate(addr *Address) {
	a.getAddress().notifyAddressOnTerminate(addr)
}

// RemoveNotifyAddress will remove the notification request from the
// Address you call this on.
//
// This does not guarantee that you will not receive a termination
// notification from the Address, due to race conditions.
func (a *Address) RemoveNotifyAddress(addr *Address) {
	a.getAddress().removeNotifyAddress(addr)
}

// MarshalBinary implements binary marshalling for Addresses.
//
// A marshalled Address only carries its identifier. When unmarshalled on
// the same node, the unmarshalled address will be reconnected to the
// original Mailbox. If unmarshalled on a different node, a reference to
// the remote mailbox will be unmarshaled.
func (a *Address) MarshalBinary() ([]byte, error) {
	addr := a.getAddress()

	switch mbox := addr.(type) {
	case *Mailbox:
		b := make([]byte, 10)
		written := binary.PutUvarint(b, uint64(mbox.id))
		return append([]byte("<"), b[:written]...), nil

	case noMailbox:
		return []byte("X"), nil

	case boundRemoteAddress:
		b := make([]byte, 10)
		written := binary.PutUvarint(b, uint64(mbox.MailboxID))
		return append([]byte("<"), b[:written]...), nil

	default:
		return nil, ErrIllegalAddressFormat
	}

}

// UnmarshalBinary implements binary unmarshalling for Addresses.
func (a *Address) UnmarshalBinary(b []byte) error {
	*a = Address{
		mailboxID:        0,
		connectionServer: connections,
	}

	if len(b) == 0 {
		return ErrIllegalAddressFormat
	}

	if b[0] == 60 { // this is "<"
		id, readBytes := binary.Uvarint(b[1:])
		if readBytes == 0 {
			return ErrIllegalAddressFormat
		}
		a.mailboxID = MailboxID(id)
		return nil
	}

	if len(b) == 1 && b[0] == 88 { // capital X
		a.mailbox = noMailbox{0}
		a.mailboxID = MailboxID(0)
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
func (a *Address) UnmarshalFromID(mID MailboxID) {
	a.mailboxID = mID
	a.mailbox = nil
	a.connectionServer = nil
}

// UnmarshalText implements text unmarshalling for Addresses.
func (a *Address) UnmarshalText(b []byte) error {
	*a = Address{
		mailboxID:        0,
		connectionServer: connections,
	}

	if b == nil {
		return errIllegalNilSlice
	}

	if len(b) == 0 {
		return ErrIllegalAddressFormat
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

		a.mailboxID = MailboxID(mailboxIDNum<<8 + nodeID)

		return nil

	case byte('X'):
		a.mailbox = noMailbox{0}
		a.mailboxID = MailboxID(0)
		return nil
	}
	return ErrIllegalAddressFormat
}

// MarshalText implements text marshalling for Addresses.
//
// See MarshalBinary.
func (a *Address) MarshalText() ([]byte, error) {
	// Cache the mailbox before we attempt to marshal the Address.
	_ = a.getAddress()

	switch mbox := a.mailbox.(type) {
	case *Mailbox:
		ClusterID := mbox.id.NodeID()
		mailboxID := mbox.id.mailboxOnlyID()
		text := fmt.Sprintf("<%d:%d>", ClusterID, mailboxID)
		return []byte(text), nil

	case noMailbox:
		return []byte("X"), nil

	case boundRemoteAddress:
		ClusterID := mbox.MailboxID.NodeID()
		mailboxID := mbox.MailboxID.mailboxOnlyID()
		text := fmt.Sprintf("<%d:%d>", ClusterID, mailboxID)
		return []byte(text), nil

	default:
		return nil, errors.New("unknown address type, internal reign error")
	}
}

// UnmarshalJSON implements JSON unmarshalling for Addresses.
func (a *Address) UnmarshalJSON(b []byte) error {
	// Replace quotes with the delimeters.
	if bytes.HasPrefix(b, []byte("\"")) {
		b[0] = byte('<')
	}
	if bytes.HasSuffix(b, []byte("\"")) {
		b[len(b)-1] = byte('>')
	}

	return a.UnmarshalText(b)
}

// MarshalJSON implements JSON marshalling for Addresses.
func (a *Address) MarshalJSON() ([]byte, error) {
	b, err := a.MarshalText()
	if err != nil {
		return nil, err
	}

	// Replace delimeters with quotes.
	if bytes.HasPrefix(b, []byte("<")) {
		b[0] = byte('"')
	}
	if bytes.HasSuffix(b, []byte(">")) {
		b[len(b)-1] = byte('"')
	}

	return b, nil
}

func (a *Address) String() string {
	b, _ := a.MarshalText()
	return string(b)
}

// MailboxID is an identifier corresponding to a mailbox.
type MailboxID uint64

// NodeID returns the node ID corresponding to the current mailbox ID.
func (mID MailboxID) NodeID() NodeID {
	return NodeID(mID & 255)
}

func (mID MailboxID) mailboxOnlyID() uint64 {
	return uint64(mID >> 8)
}

// MailboxTerminated is sent to Addresses that request notification
// of when a Mailbox is being terminated, with NotifyAddressOnTerminate.
// If you request termination notification of multiple mailboxes, this can
// be converted to an MailboxID which can be used to distinguish them.
type MailboxTerminated MailboxID

type mailboxes struct {
	nextMailboxID MailboxID
	nodeID        NodeID

	// this isn't an ideal data structure. It's enough to satisfy the author's
	// use case, but if you throw "enough" cores at this and create mailboxes
	// rapidly enough, this could start to become a bottleneck.
	// Still, this *is* only touched at creation and deletion of mailboxes,
	// not on every message or anything.
	mailboxes map[MailboxID]*Mailbox

	connectionServer *connectionServer
	sync.RWMutex
}

func (m *mailboxes) newLocalMailbox() (*Address, *Mailbox) {
	var mutex sync.Mutex
	cond := sync.NewCond(&mutex)

	nextID := MailboxID(atomic.AddUint64((*uint64)(&m.nextMailboxID), 1))
	m.nextMailboxID++
	id := nextID<<8 + MailboxID(m.nodeID)

	mailbox := &Mailbox{
		id:       id,
		messages: make([]message, 0, 1),
		cond:     cond,
		parent:   m,
	}

	m.registerMailbox(id, mailbox)
	addr := &Address{
		mailboxID:        id,
		mailbox:          mailbox,
		connectionServer: m.connectionServer,
	}

	return addr, mailbox
}

// it is an error to call this with the same mID more than once
func (m *mailboxes) registerMailbox(mID MailboxID, mbox *Mailbox) {
	m.Lock()
	defer m.Unlock()

	m.mailboxes[mID] = mbox
}

func (m *mailboxes) unregisterMailbox(mID MailboxID) {
	m.Lock()
	defer m.Unlock()

	delete(m.mailboxes, mID)
}

func (m *mailboxes) sendByID(mID MailboxID, msg interface{}) error {
	mailbox, err := m.mailboxByID(mID)
	if err != nil {
		return err
	}
	return mailbox.send(msg)
}

func (m *mailboxes) mailboxByID(mID MailboxID) (*Mailbox, error) {
	if mID.NodeID() != m.nodeID {
		return nil, ErrNotLocalMailbox
	}

	m.RLock()
	mbox, exists := m.mailboxes[mID]
	m.RUnlock()

	if !exists {
		return nil, ErrMailboxTerminated
	}

	return mbox, nil
}

// Returns a new set of mailboxes. This is used by the clustering
// code. Users would not normally call this.
func newMailboxes(connectionServer *connectionServer, nodeID NodeID) *mailboxes {
	return &mailboxes{
		nextMailboxID:    1,
		nodeID:           nodeID,
		connectionServer: connectionServer,
		mailboxes:        make(map[MailboxID]*Mailbox),
	}
}

// A Mailbox is what you receive messages from via Receive or ReceiveNext.
type Mailbox struct {
	id                    MailboxID
	messages              []message
	cond                  *sync.Cond
	notificationAddresses map[MailboxID]struct{}

	// used only by testing, to implement the ability to block until
	// a notification has been processed
	parent               *mailboxes
	broadcastOnAddNotify bool
	terminated           bool
}

func (m *Mailbox) send(msg interface{}) error {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if m.terminated {
		return ErrMailboxTerminated
	}

	m.messages = append(m.messages, message{msg})

	m.cond.Broadcast()

	return nil
}

func (m *Mailbox) canBeGloballyRegistered() bool {
	return true
}

func (m *Mailbox) canBeGloballyUnregistered() bool {
	return true
}

func (m *Mailbox) getMailboxID() MailboxID {
	return m.id
}

func (m *Mailbox) notifyAddressOnTerminate(target *Address) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if m.terminated {
		_ = target.Send(MailboxTerminated(m.id))

		return
	}

	if m.notificationAddresses == nil {
		m.notificationAddresses = make(map[MailboxID]struct{})
	}
	m.notificationAddresses[target.mailboxID] = struct{}{}

	if m.broadcastOnAddNotify {
		m.cond.Broadcast()
	}
}

// This test function will block until the target address is in
// this localAddressImpl's notificationAddresses.
//
// Since this is a test-only function, we only implement enough to let
// one listen on a given address occur at a time.
func (m *Mailbox) blockUntilNotifyStatus(target *Address, desired bool) {
	m.cond.L.Lock()

	m.broadcastOnAddNotify = true

	id := target.mailboxID

	_, exists := m.notificationAddresses[id]
	for exists != desired {
		m.cond.Wait()
		_, exists = m.notificationAddresses[id]
	}
	m.broadcastOnAddNotify = false
	m.cond.L.Unlock()
}

func (m *Mailbox) removeNotifyAddress(target *Address) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if m.notificationAddresses != nil {
		delete(m.notificationAddresses, target.mailboxID)
	}
}

// MessageCount returns the number of messages in the mailbox.
//
// 0 is always returned if the mailbox is terminated.
func (m *Mailbox) MessageCount() int {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if !m.terminated {
		return len(m.messages)
	}

	return 0
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
	defer m.cond.L.Unlock()

	for len(m.messages) == 0 && !m.terminated {
		m.cond.Wait()
	}

	if m.terminated {
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

	return msg.msg
}

// ReceiveNextAsync will return immediately with (obj, true) if, and only if,
// there was a message in the inbox, or else (nil, false). Works the same way
// as ReceiveNext, otherwise
func (m *Mailbox) ReceiveNextAsync() (interface{}, bool) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if len(m.messages) == 0 && !m.terminated {
		return nil, false
	}

	if m.terminated {
		return MailboxTerminated(m.id), true
	}

	msg := m.messages[0]
	// in the common case of not having a message backlog, this
	// should prevent a lot of garbage buildup by reusing the slot.
	if len(m.messages) == 1 {
		m.messages = m.messages[:0]
	} else {
		m.messages = m.messages[1:]
	}
	return msg.msg, true
}

// ReceiveNextTimeout works like ReceiveNextAsync, but it will wait until either a message
// is received or the timeout expires, whichever is sooner
func (m *Mailbox) ReceiveNextTimeout(timeout time.Duration) (interface{}, bool) {
	startTime := time.Now()
	endTime := startTime.Add(timeout)

	for !time.Now().After(endTime) {
		msg, ok := m.ReceiveNextAsync()
		if ok {
			return msg, ok
		}
	}

	return nil, false
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
//  func (i) bool {
//      _, ok = i.(SomeType)
//
//      return ok
//  }
//
// If the mailbox gets terminated, this will return a MailboxTerminated,
// regardless of the behavior of the matcher.
func (m *Mailbox) Receive(matcher func(interface{}) bool) interface{} {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	if m.terminated {
		return MailboxTerminated(m.id)
	}

	// see if there are any messages that match
	for i, v := range m.messages {
		if matcher(v.msg) {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
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
			return MailboxTerminated(m.id)
		}

		for ; lastIdx < len(m.messages); lastIdx++ {
			if matcher(m.messages[lastIdx].msg) {
				match := m.messages[lastIdx].msg
				m.messages = append(m.messages[:lastIdx], m.messages[lastIdx+1:]...)
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
	defer m.cond.L.Unlock()

	// this is not redundant; m.terminated is part of what we have to lock
	if m.terminated {
		return
	}

	m.terminated = true

	terminating := MailboxTerminated(m.id)
	cs := m.parent.connectionServer
	for mailboxID := range m.notificationAddresses {
		addr := Address{
			mailboxID:        mailboxID,
			connectionServer: cs,
		}
		_ = addr.Send(terminating)
	}

	// chuck out what garbage we can
	m.notificationAddresses = nil
	m.messages = nil

	m.cond.Broadcast()
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
	MailboxID
}

func (nm noMailbox) send(interface{}) error {
	return ErrMailboxTerminated
}

func (nm noMailbox) notifyAddressOnTerminate(target *Address) {
	_ = target.Send(MailboxTerminated(nm.MailboxID))
}

func (nm noMailbox) removeNotifyAddress(target *Address) {}

func (nm noMailbox) getMailboxID() MailboxID {
	return nm.MailboxID
}

func (nm noMailbox) canBeGloballyRegistered() bool {
	return false
}

func (nm noMailbox) canBeGloballyUnregistered() bool {
	return true
}

// A boundRemoteAddress is only used for testing in the "multinode"
// configuration. This allows us to switch into the correct node context
// when sending messages to the target mailbox, which allows us to ensure
// that we are correctly simulating the network send.
//
// FIXME: Too much indirection here, there's no reason not to bind this
// directly to the target address.
type boundRemoteAddress struct {
	MailboxID
	*remoteMailboxes
}

func (bra boundRemoteAddress) send(message interface{}) error {
	// FIMXE: Have to pass along the mailboxID here.
	return bra.remoteMailboxes.Send(
		internal.OutgoingMailboxMessage{
			Target:  internal.IntMailboxID(bra.MailboxID),
			Message: message,
		},
	)
}

func (bra boundRemoteAddress) notifyAddressOnTerminate(addr *Address) {
	// as this is internal only, we can just hard-assert the local address
	// is a "real" mailbox
	_ = bra.remoteMailboxes.Send(
		internal.NotifyRemote{
			Remote: internal.IntMailboxID(bra.MailboxID),
			Local:  internal.IntMailboxID(addr.mailboxID),
		},
	)
}

func (bra boundRemoteAddress) removeNotifyAddress(addr *Address) {
	// as this is internal only, we can just hard-assert the local address
	// is a "real" mailbox
	_ = bra.remoteMailboxes.Send(
		internal.UnnotifyRemote{
			Remote: internal.IntMailboxID(bra.MailboxID),
			Local:  internal.IntMailboxID(addr.mailboxID),
		},
	)
}

func (bra boundRemoteAddress) getMailboxID() MailboxID {
	return bra.MailboxID
}

func (bra boundRemoteAddress) canBeGloballyRegistered() bool {
	return false
}

func (bra boundRemoteAddress) canBeGloballyUnregistered() bool {
	return false
}

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
	"context"
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

var (
	// ErrIllegalAddressFormat is returned when something attempts to
	// unmarshal an illegal text or binary string into an Address.
	ErrIllegalAddressFormat = errors.New("illegally-formatted address")

	// ErrIllegalNilSlice is returned when UnmarshalText is called with a nil byte slice.
	ErrIllegalNilSlice = errors.New("cannot unmarshal nil slice into an address")

	// ErrMailboxClosed is returned when the target mailbox has (already) been closed.
	ErrMailboxClosed = errors.New("mailbox has been closed")

	// ErrNotLocalMailbox is returned when a remote mailbox's MailboxID is passed
	// into a function that only works on local mailboxes.
	ErrNotLocalMailbox = errors.New("function required a local mailbox ID but this is a remote MailboxID")
)

type address interface {
	send(interface{}) error

	onCloseNotify(*Address)
	removeNotify(*Address)
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
// testing or as a key in maps! Use .GetID() to obtain a MailboxID, which
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
		// error is ErrMailboxClosed. While this is in some
		// sense an error for the user, as far as marshaling is
		// concerned, this is success, because it's perfectly legal to
		// unmarshal an address corresponding to something that has
		// since closed, just as it's perfectly legal to hold on to
		// a reference to a mailbox that has closed. We do however
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

// Send something to the mailbox corresponding to this address.
//
// All concrete types that you wish to send across the cluster must
// have .Register called on them. See the documentation on gob.Register
// for the reason why. (The local .RegisterType abstracts our dependency on
// gob. If you don't register through reign's .RegisterType, future versions
// of this package may require you to fix that.)
//
// The error is primarily for internal purposes. If the mailbox is
// local, and has been closed, ErrMailboxClosed will be
// returned.
//
// An error guarantees failure, but lack of error does not guarantee
// success! Arguably, "ErrMailboxClosed" should be seen as a purely
// internal detail, and just like in Erlang, if you want a guarantee
// you must implement an acknowledgement. However, just like in
// Erlang, we leak this internal detail a bit. I don't know if
// that's a good idea; use with caution. (See: erlang:is_process_alive,
// which similarly leaks out whether the process is local or not.)
func (a *Address) Send(m interface{}) error {
	return a.getAddress().send(m)
}

// OnCloseNotify requests that the target address receive a
// close notice when the target address is closed.
//
// This is like linking in Erlang, and is intended to provide the
// same guarantees.
//
// While addresses and goroutines are not technically bound together,
// it is convenient to think of an address "belonging" to a goroutine.
// From that point of view, note the caller is the *argument*, not
// the object. Calls look like:
//
//     otherAddress.OnCloseNotify(myAddress)
//
// Read that as something like, "You over there, upon your closing notify
// (me)."
//
// Calling this more than once with the same address may or may not
// cause multiple notifications to occur.
func (a *Address) OnCloseNotify(addr *Address) {
	a.getAddress().onCloseNotify(addr)
}

// RemoveNotify will remove the notification request from the
// Address you call this on.
//
// This does not guarantee that you will not receive a closed
// notification from the Address, due to (inherent) race conditions.
func (a *Address) RemoveNotify(addr *Address) {
	a.getAddress().removeNotify(addr)
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
		return ErrIllegalNilSlice
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

// MailboxClosed is sent to Addresses that request notification
// of when a Mailbox is being closed, with OnCloseNotify.
// If you request close notification of multiple mailboxes, this can
// be converted to an MailboxID which can be used to distinguish them.
type MailboxClosed MailboxID

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
	nextID := MailboxID(atomic.AddUint64((*uint64)(&m.nextMailboxID), 1))
	m.nextMailboxID++
	id := nextID<<8 + MailboxID(m.nodeID)

	mailbox := &Mailbox{
		id:          id,
		messages:    make([]message, 0, 1),
		nextMessage: make(chan message, 1),
		parent:      m,
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
		return nil, ErrMailboxClosed
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
	id     MailboxID
	parent *mailboxes

	mu                    sync.Mutex
	messages              []message
	nextMessage           chan message
	notificationAddresses map[MailboxID]struct{}
	closed                bool
}

func (m *Mailbox) send(msg interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	select {
	case prevMsg, ok := <-m.nextMessage:
		if !ok {
			return ErrMailboxClosed
		}
		m.messages = append(m.messages, message{msg})
		m.nextMessage <- prevMsg
	default:
		m.messages = append(m.messages, message{msg})
		m.nextMessage <- m.messages[0]
		m.messages = m.messages[1:]
	}

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

func (m *Mailbox) onCloseNotify(target *Address) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		_ = target.Send(MailboxClosed(m.id))

		return
	}

	if m.notificationAddresses == nil {
		m.notificationAddresses = make(map[MailboxID]struct{})
	}

	m.notificationAddresses[target.mailboxID] = struct{}{}
}

// This test function will block until the target address is in
// this localAddressImpl's notificationAddresses.
//
// Since this is a test-only function, we only implement enough to let
// one listen on a given address occur at a time.
func (m *Mailbox) blockUntilNotifyStatus(target *Address, desired bool, interval time.Duration) {
	id := target.mailboxID

	m.mu.Lock()
	_, exists := m.notificationAddresses[id]
	m.mu.Unlock()

	for exists != desired {
		// TODO: Block here until a new message is received instead of checking on an interval.
		time.Sleep(interval)
		m.mu.Lock()
		_, exists = m.notificationAddresses[id]
		m.mu.Unlock()
	}
}

func (m *Mailbox) removeNotify(target *Address) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.notificationAddresses != nil {
		delete(m.notificationAddresses, target.mailboxID)
	}
}

// MessageCount returns the number of messages in the mailbox.
//
// 0 is always returned if the mailbox is terminated.
func (m *Mailbox) MessageCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		return len(m.messages)
	}

	return 0
}

// Receive will receive the next message sent to this mailbox.
// It blocks until the next message comes in, which may be forever.
// If the mailbox is closed, it will receive a MailboxClosed reply.
//
// If you've got multiple receivers on a single mailbox, be sure to check
// for MailboxClosed.
func (m *Mailbox) Receive(ctx context.Context) (interface{}, error) {
	// FIXME: Verify three listeners on one shared mailbox all get
	// closed properly.
	var (
		ok  bool
		msg message
	)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok = <-m.nextMessage:
		if !ok {
			return MailboxClosed(m.id), ErrMailboxClosed
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.messages) > 0 {
		select {
		case m.nextMessage <- m.messages[0]:
			m.messages = m.messages[1:]
		default:
		}
	}

	return msg.msg, nil
}

// ReceiveAsync will return immediately with (obj, true) if, and only if,
// there was a message in the inbox, or else (nil, false). Works the same way
// as Receive otherwise.
func (m *Mailbox) ReceiveAsync() (interface{}, bool) {
	var (
		ok  bool
		msg message
	)

	select {
	case msg, ok = <-m.nextMessage:
		if !ok {
			return MailboxClosed(m.id), true
		}
	default:
		return nil, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.messages) > 0 {
		select {
		case m.nextMessage <- m.messages[0]:
			m.messages = m.messages[1:]
		default:
		}
	}

	return msg.msg, true
}

// ReceiveMatch will receive the next message sent to this mailbox that matches
// according to the passed-in function.
//
// ReceiveMatch assumes that it is the only function running against the
// Mailbox. If you ReceiveMatch from multiple goroutines, or ReceiveMatch in one
// and ReceiveNext in another, you *will* miss messages in the routine
// calling ReceiveMatch.
//
// I recommend that your matcher function be:
//
//  func (i) bool {
//      _, ok = i.(SomeType)
//
//      return ok
//  }
//
// If the mailbox gets closed, this will return a MailboxClosed,
// regardless of the behavior of the matcher.
func (m *Mailbox) ReceiveMatch(ctx context.Context, matcher func(interface{}) bool) (interface{}, error) {
	var (
		ok  bool
		msg message
	)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg, ok = <-m.nextMessage:
			if !ok {
				return MailboxClosed(m.id), ErrMailboxClosed
			}
		}

		m.mu.Lock()

		// Make sure the mailbox was not closed between reading the
		// next message and acquiring the mutex.
		select {
		case prevMsg, ok := <-m.nextMessage:
			if !ok {
				return MailboxClosed(m.id), ErrMailboxClosed
			}
			// Account for any message put on the nextMessage channel
			// between the last select and acquiring the mutex above.
			if prevMsg.msg != nil {
				m.messages = append([]message{prevMsg}, m.messages...)
			}
		default:
		}

		// Does the message we just pulled off the channel match?
		if matcher(msg.msg) {
			if len(m.messages) > 0 {
				select {
				case m.nextMessage <- m.messages[0]:
					m.messages = m.messages[1:]
				default:
				}
			}

			m.mu.Unlock()

			return msg.msg, nil
		}

		m.nextMessage <- msg

		// No match.  Look through the messages in the queue.
		for i := 0; i < len(m.messages); i++ {
			if matcher(m.messages[i].msg) {
				match := m.messages[i].msg
				m.messages = append(m.messages[:i], m.messages[i+1:]...)

				m.mu.Unlock()

				return match, nil
			}
		}

		m.mu.Unlock()
	}
}

// Close shuts down a given mailbox. Once closed, a mailbox
// will reject messages without even looking at them, and can no longer
// have any Receive used on them.
//
// Further, it will notify any registered Addresses that it has been closed.
//
// This facility is used analogously to Erlang's "link" functionality.
// Of course in Go you can't be notified when a goroutine terminates, but
// if you defer mailbox.Close() in the proper place for your mailbox
// user, you can get most of the way there.
//
// It is not an error to Close an already-Closed mailbox.
func (m *Mailbox) Close() {
	// I think doing this before locking our own lock is correct; we are
	// already uninterested in any future operations, and double-deleting
	// out of this dict is OK.
	m.parent.unregisterMailbox(m.id)

	m.mu.Lock()
	defer m.mu.Unlock()

	select {
	case _, ok := <-m.nextMessage:
		if !ok {
			return
		}
	default:
	}

	close(m.nextMessage)
	m.closed = true
	closing := MailboxClosed(m.id)
	cs := m.parent.connectionServer

	for mailboxID := range m.notificationAddresses {
		addr := Address{
			mailboxID:        mailboxID,
			connectionServer: cs,
		}
		_ = addr.Send(closing)
	}

	// chuck out what garbage we can
	m.notificationAddresses = nil
	m.messages = nil
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
	return ErrMailboxClosed
}

func (nm noMailbox) onCloseNotify(target *Address) {
	_ = target.Send(MailboxClosed(nm.MailboxID))
}

func (nm noMailbox) removeNotify(_ *Address) {}

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

func (bra boundRemoteAddress) onCloseNotify(addr *Address) {
	// as this is internal only, we can just hard-assert the local address
	// is a "real" mailbox
	_ = bra.remoteMailboxes.Send(
		internal.NotifyRemote{
			Remote: internal.IntMailboxID(bra.MailboxID),
			Local:  internal.IntMailboxID(addr.mailboxID),
		},
	)
}

func (bra boundRemoteAddress) removeNotify(addr *Address) {
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

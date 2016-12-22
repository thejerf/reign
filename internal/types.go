/*

Package internal segments off things that must be public for serialization
but have no place in the main documentation.

Some types are replicated as they can't be imported from the main reign
package due to circular imports.

*/
package internal

import (
	"encoding/gob"
)

func init() {
	var nnot NotifyNodeOnTerminate
	var _ ClusterMessage = (*NotifyNodeOnTerminate)(nil)
	gob.Register(&nnot)

	var rmt RemoteMailboxTerminated
	var _ ClusterMessage = (*RemoteMailboxTerminated)(nil)
	gob.Register(&rmt)

	var rnnot RemoveNotifyNodeOnTerminate
	var _ ClusterMessage = (*RemoveNotifyNodeOnTerminate)(nil)
	gob.Register(&rnnot)

	var omm OutgoingMailboxMessage
	var _ ClusterMessage = (*OutgoingMailboxMessage)(nil)
	gob.Register(&omm)

	var imm IncomingMailboxMessage
	var _ ClusterMessage = (*IncomingMailboxMessage)(nil)
	gob.Register(&imm)

	var ph PanicHandler
	var _ ClusterMessage = (*PanicHandler)(nil)
	gob.Register(&ph)

	var ping Ping
	var _ ClusterMessage = (*Ping)(nil)
	gob.Register(&ping)

	var pong Pong
	var _ ClusterMessage = (*Pong)(nil)
	gob.Register(&pong)
}

// IntNodeID reflects the NodeID type in the main package.
type IntNodeID byte

// IntMailboxID reflects the internal mailboxID type.
type IntMailboxID uint64

// NodeID returns the node ID corresponding to the current mailbox ID.
func (mID IntMailboxID) NodeID() IntNodeID {
	return IntNodeID(mID & 255)
}

// AllNodeClaims is part of the internal registry's private communication.
type AllNodeClaims struct {
	Node   IntNodeID
	Claims map[string]map[IntMailboxID]struct{}
}

// RegistrySync is part of the internal registry's private communication.
type RegistrySync struct {
	Node      IntNodeID
	MailboxID IntMailboxID
	Claims    AllNodeClaims
}

// RegistryMailbox is sent between node registries to populate their
// nodeRegistries map.
type RegistryMailbox struct {
	Node      IntNodeID
	MailboxID IntMailboxID
}

// RegisterName is part of the internal registry's private communication.
type RegisterName struct {
	Node      IntNodeID
	Name      string
	MailboxID IntMailboxID
}

// UnregisterName is part of the internal registry's private communication.
type UnregisterName struct {
	Node      IntNodeID
	Name      string
	MailboxID IntMailboxID
}

// UnregisterMailbox is part of the internal registry's private communication.
type UnregisterMailbox struct {
	Node      IntNodeID
	MailboxID IntMailboxID
}

// ClusterHandshake is part of the cluster connection process.
type ClusterHandshake struct {
	ClusterVersion uint16
	MyNodeID       IntNodeID
	YourNodeID     IntNodeID
}

// ClusterMessage is a tag used to identify messages the cluster can send
// across the wire.
type ClusterMessage interface {
	isClusterMessage()
}

// NotifyNodeOnTerminate is an internal message, public only for
// gob's sake.
type NotifyNodeOnTerminate struct {
	IntMailboxID
}

func (nnot *NotifyNodeOnTerminate) isClusterMessage() {}

// RemoveNotifyNodeOnTerminate is an internal message, public only for
// gob's sake.
type RemoveNotifyNodeOnTerminate struct {
	IntMailboxID
}

func (rnnot *RemoveNotifyNodeOnTerminate) isClusterMessage() {}

// RemoteMailboxTerminated is an internal message, public only for gob's
// sake.
type RemoteMailboxTerminated struct {
	IntMailboxID
}

func (rmt *RemoteMailboxTerminated) isClusterMessage() {}

// DestroyConnection is used internally to simulate connection loss.
type DestroyConnection struct{}

func (dc DestroyConnection) isClusterMessage() {}

// OutgoingMailboxMessage indicates that this wraps an outgoing message.
type OutgoingMailboxMessage struct {
	Target  IntMailboxID
	Message interface{}
}

func (omm OutgoingMailboxMessage) isClusterMessage() {}

// IncomingMailboxMessage indicates the embedded message is destined for a local mailbox.
type IncomingMailboxMessage struct {
	Target  IntMailboxID
	Message interface{}
}

func (imm IncomingMailboxMessage) isClusterMessage() {}

type NotifyRemote struct {
	Local  IntMailboxID
	Remote IntMailboxID
}

func (lr NotifyRemote) isClusterMessage() {}

type UnnotifyRemote struct {
	Local  IntMailboxID
	Remote IntMailboxID
}

func (ur UnnotifyRemote) isClusterMessage() {}

// PanicHandler can be sent over the network to cause the receiver
// to panic, simulating whatever may end up doing that.
type PanicHandler struct{}

func (ph PanicHandler) isClusterMessage() {}

// Ping is sent in order to keep the network connection open.
type Ping struct{}

func (p Ping) isClusterMessage() {}

// Pong is sent in reply to a Ping message.
type Pong struct{}

func (p Pong) isClusterMessage() {}

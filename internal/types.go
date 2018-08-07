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
	var anc AllNodeClaims
	var _ ClusterMessage = (*AllNodeClaims)(nil)
	gob.Register(&anc)

	var dc DestroyConnection
	var _ ClusterMessage = (*DestroyConnection)(nil)
	gob.Register(&dc)

	var imm IncomingMailboxMessage
	var _ ClusterMessage = (*IncomingMailboxMessage)(nil)
	gob.Register(&imm)

	var nnot NotifyNodeOnTerminate
	var _ ClusterMessage = (*NotifyNodeOnTerminate)(nil)
	gob.Register(&nnot)

	var nr NotifyRemote
	var _ ClusterMessage = (*NotifyRemote)(nil)
	gob.Register(&nr)

	var omm OutgoingMailboxMessage
	var _ ClusterMessage = (*OutgoingMailboxMessage)(nil)
	gob.Register(&omm)

	var ph PanicHandler
	var _ ClusterMessage = (*PanicHandler)(nil)
	gob.Register(&ph)

	var ping Ping
	var _ ClusterMessage = (*Ping)(nil)
	gob.Register(&ping)

	var pong Pong
	var _ ClusterMessage = (*Pong)(nil)
	gob.Register(&pong)

	var rn RegisterName
	var _ ClusterMessage = (*RegisterName)(nil)
	gob.Register(&rn)

	var rm RegistryMailbox
	var _ ClusterMessage = (*RegistryMailbox)(nil)
	gob.Register(&rm)

	var rs RegisterRemoteNode
	var _ ClusterMessage = (*RegisterRemoteNode)(nil)
	gob.Register(&rs)

	var rmt RemoteMailboxTerminated
	var _ ClusterMessage = (*RemoteMailboxTerminated)(nil)
	gob.Register(&rmt)

	var rnnot RemoveNotifyNodeOnTerminate
	var _ ClusterMessage = (*RemoveNotifyNodeOnTerminate)(nil)
	gob.Register(&rnnot)

	var ur UnnotifyRemote
	var _ ClusterMessage = (*UnnotifyRemote)(nil)
	gob.Register(&ur)

	var um UnregisterMailbox
	var _ ClusterMessage = (*UnregisterMailbox)(nil)
	gob.Register(&um)

	var un UnregisterName
	var _ ClusterMessage = (*UnregisterName)(nil)
	gob.Register(&un)
}

// ClusterHandshake is part of the cluster connection process.
// This type is sent directly.  Therefore, it does not need to
// implement the ClusterMessage interface, nor be registered with
// gob.
type ClusterHandshake struct {
	ClusterVersion uint16
	MyNodeID       IntNodeID
	YourNodeID     IntNodeID
}

// IntNodeID reflects the NodeID type in the main package.
type IntNodeID byte

// IntMailboxID reflects the internal mailboxID type.
type IntMailboxID uint64

// NodeID returns the node ID corresponding to the current mailbox ID.
func (mID IntMailboxID) NodeID() IntNodeID {
	return IntNodeID(mID & 255)
}

// ClusterMessage is a tag used to identify messages the cluster can send
// across the wire.
type ClusterMessage interface {
	isClusterMessage()
}

// AllNodeClaims is part of the internal registry's private communication.
type AllNodeClaims struct {
	Node   IntNodeID
	Claims map[string]map[IntMailboxID]struct{}
}

func (anc *AllNodeClaims) isClusterMessage() {}

// DestroyConnection is used internally to simulate connection loss.
type DestroyConnection struct{}

func (dc DestroyConnection) isClusterMessage() {}

// IncomingMailboxMessage indicates the embedded message is destined for a local mailbox.
type IncomingMailboxMessage struct {
	Target  IntMailboxID
	Message interface{}
}

func (imm IncomingMailboxMessage) isClusterMessage() {}

// NotifyNodeOnTerminate is an internal message, public only for
// gob's sake.
type NotifyNodeOnTerminate struct {
	IntMailboxID
}

func (nnot *NotifyNodeOnTerminate) isClusterMessage() {}

// NotifyRemote is an internal message, public only for gob's sake.
type NotifyRemote struct {
	Local  IntMailboxID
	Remote IntMailboxID
}

func (lr NotifyRemote) isClusterMessage() {}

// OutgoingMailboxMessage indicates that this wraps an outgoing message.
type OutgoingMailboxMessage struct {
	Target  IntMailboxID
	Message interface{}
}

func (omm OutgoingMailboxMessage) isClusterMessage() {}

// PanicHandler can be sent over the network to cause the receiver
// to panic, simulating whatever may end up doing that.
type PanicHandler struct{}

func (ph PanicHandler) isClusterMessage() {}

// Ping is sent in order to keep the network connection open.
type Ping struct{}

func (p Ping) isClusterMessage() {}

// Pong is sent in reply to a Ping message.  When received, the connection
// deadline is progressed.
type Pong struct{}

func (p Pong) isClusterMessage() {}

// RegisterName is part of the internal registry's private communication.
type RegisterName struct {
	Node      IntNodeID
	Name      string
	MailboxID IntMailboxID
}

func (rn *RegisterName) isClusterMessage() {}

// RegistryMailbox is sent between node registries to populate their
// nodeRegistries map.
type RegistryMailbox struct {
	Node      IntNodeID
	MailboxID IntMailboxID
}

func (rm *RegistryMailbox) isClusterMessage() {}

// RegisterRemoteNode is part of the internal registry's private communication.
type RegisterRemoteNode struct {
	Node      IntNodeID
	MailboxID IntMailboxID
}

func (rs *RegisterRemoteNode) isClusterMessage() {}

// RemoteMailboxTerminated is an internal message, public only for gob's
// sake.
type RemoteMailboxTerminated struct {
	IntMailboxID
}

func (rmt *RemoteMailboxTerminated) isClusterMessage() {}

// RemoveNotifyNodeOnTerminate is an internal message, public only for
// gob's sake.
type RemoveNotifyNodeOnTerminate struct {
	IntMailboxID
}

func (rnnot *RemoveNotifyNodeOnTerminate) isClusterMessage() {}

// UnnotifyRemote is an internal message, public only for gob's sake.
type UnnotifyRemote struct {
	Local  IntMailboxID
	Remote IntMailboxID
}

func (ur UnnotifyRemote) isClusterMessage() {}

// UnregisterMailbox is part of the internal registry's private communication.
type UnregisterMailbox struct {
	Node      IntNodeID
	MailboxID IntMailboxID
}

func (um *UnregisterMailbox) isClusterMessage() {}

// UnregisterName is part of the internal registry's private communication.
type UnregisterName struct {
	Node      IntNodeID
	Name      string
	MailboxID IntMailboxID
}

func (un *UnregisterName) isClusterMessage() {}

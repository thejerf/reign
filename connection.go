package reign

import (
	"fmt"
	"time"

	"github.com/thejerf/reign/internal"
	"github.com/thejerf/suture"
)

// This manages the cluster's communication channels.
//
// The cluster works by creating a full mesh of connections. Thus, exactly
// like Erlang, this design tends to break down at a certain size.
//
// The protocol is this:
//
// * Each of the nodes pairs forms a "Connection".
// * The node in a Connection with the LOWER NodeID is responsible for
//   establishing the connection to the other node, which it will try
//   to maintain.
// * The connecting node performs a SSL handshake. Both sides provide
//   their certificates to the other, and both sides verify that the
//   certificate has been signed.
//
//   The nodes then validate over SSL that they have the same cluster definition.
//
// * Having established the connection, the Clustering handshake is then
//   exchanged. This contains version information for the clustering protocol,
//   and the hash of each side's clustering information, and the node IDs
//   each side thinks they are talking to. If the nodes don't match, it's a
//   FATAL error for the connection. If the HASH of the cluster info doesn't
//   match, it's a non-fatal error that issues a warning.
//
//   At this point, one of two things happen:
//   1. The nodes are both in the same "Group", in which case they
//      drop the SSL and return to using the socket directly.
//   2. The nodes are in different Groups, and retain their SSL connection.
//      Additionally, "large" messages will be gzip'ed.
//
// * Finally, we begin the clustering itself, which consists of sending messages
//   across.
//
// Since we use gob encoding, we inherit two major aspects of the gob system:
//
// * Most versioning is dealt with in the gob manner of just sort of flinging
//   struct members in the relevant places and hoping for the best. While
//   this makes me personally a bit uncomfortable, it seems to be the Go way,
//   and I'm not going to fight it.
// * All types of messages that are going to be sent need to be Registered
//   with the gob encoder, which is somewhat inconvenient. Reign will take
//   care of automatically registering all of its messages, but you'll need
//   to provide a registration of all types of messages you send to a mailbox.
//   (Since mailboxes use interface{}, this means ALL messages you might
//   send.)

type voidtype struct{}

var void = voidtype{}

// ConnectionService provides an interface to the reign connectionServer and registry objects. It
// inherits from suture.Service and reign.Cluster.
type ConnectionService interface {
	NewMailbox() (*Address, *Mailbox)
	Terminate()

	// Inherited from suture.Service
	Serve()
	Stop()

	// Inherited from reign.Cluster
	AddConnectionStatusCallback(f func(NodeID, bool))
}

// A connection serve manages the connections, both incoming and outgoing.
// So it maintains a listener (if necessary), and maintains the outgoing
// connections. This could, arguably, be named "node".
type connectionServer struct {
	listener *nodeListener

	remoteMailboxes map[NodeID]*remoteMailboxes

	// referenced only by tests
	nodeConnectors map[NodeID]*nodeConnector

	// This supervises the nodeListener, and any nodeConnections
	// it decides to make.
	*suture.Supervisor
	*mailboxes

	registry *registry

	*Cluster
}

// NewMailbox creates a new tied pair of Address and Mailbox.
//
// A Mailbox MUST have .Terminate() called on it when you are done with
// it. Otherwise termination notifications will not properly fire and
// resources will leak.
//
// It is not safe to copy a Mailbox by value; client code should never have
// Mailbox appearing as a non-pointer-type in its code. It is a code smell
// to have *Mailbox used as a map key; use AddressIDs instead.
func (cs *connectionServer) NewMailbox() (*Address, *Mailbox) {
	return cs.newLocalMailbox()
}

func (cs *connectionServer) getNodes() []NodeID {
	nodes := []NodeID{}
	for nodeID := range cs.nodeConnectors {
		nodes = append(nodes, nodeID)
	}
	return nodes
}

// this is a function that allows tests to wait for a cluster connection
// to be established to the target node before continuing on.
func (cs *connectionServer) waitForConnection(node NodeID) {
	cs.remoteMailboxes[node].waitForConnection()
}

// used only by tests; see the nodeListener method of the same name.
func (cs *connectionServer) waitForListen() {
	// nodeListener handles the nil case
	cs.listener.waitForListen()
}

func (cs *connectionServer) Terminate() {
	if cs.registry != nil {
		cs.registry.Terminate()
	}

	setConnections(nil)
}

func (cs *connectionServer) send(mID MailboxID, msg interface{}) error {
	var err error

	if mID.NodeID() == cs.ThisNode.ID {
		err = cs.mailboxes.sendByID(mID, msg)
	} else {
		err = cs.remoteMailboxes[mID.NodeID()].send(
			internal.OutgoingMailboxMessage{
				Target:  internal.IntMailboxID(mID),
				Message: msg,
			},
		)
	}

	return err
}

func newConnections(cluster *Cluster, myNodeID NodeID) *connectionServer {
	if cluster == nil {
		panic("nil cluster passed to newConnections")
	}
	if cluster.ClusterLogger == nil {
		panic("nil cluster logger passed to newConnections")
	}
	// by construction, any call that gets us here will only be for a node
	// in the cluster.
	myNode := cluster.Nodes[myNodeID]

	newConnections := &connectionServer{
		Cluster:        cluster,
		nodeConnectors: make(map[NodeID]*nodeConnector),
	}
	newConnections.mailboxes = newMailboxes(newConnections, myNodeID)
	newConnections.registry = newRegistry(newConnections, myNodeID, cluster.ClusterLogger)

	l := cluster.ClusterLogger

	logFunc := func(msg string) {
		l.Warn(msg)
	}

	// This specifies that we try fairly frequently to reconnect from a
	// human point of view, but in terms of network traffic this is a
	// very minimal amount of load. Plus the delay is augmented by the
	// total timeout before we give up on a connection.
	newConnections.Supervisor = suture.New(
		fmt.Sprintf("Cluster manager for node %d", byte(myNodeID)),
		suture.Spec{
			Log:              logFunc,
			FailureDecay:     60,
			FailureThreshold: float64(len(cluster.Nodes)/2) + 1,
			FailureBackoff:   time.Second,
		},
	)
	newConnections.Add(newConnections.registry)

	var needListener = false

	newConnections.remoteMailboxes = make(map[NodeID]*remoteMailboxes)
	for nodeID, nodeDef := range cluster.Nodes {
		if myNodeID >= nodeID {
			if myNodeID != nodeID {
				// need to listen if there are any cluster elements that
				// expect to connect to us. If we are the highest node,
				// then we don't have to bother with a listener.
				needListener = true
				newConnections.remoteMailboxes[nodeID] = newRemoteMailboxes(newConnections, newConnections.mailboxes, l, myNodeID)
				newConnections.Add(newConnections.remoteMailboxes[nodeID])
			}
			// myNodeID == nodeID falls out here, we do nothing
			continue
		}

		nodeRemoteMailboxes := newRemoteMailboxes(newConnections, newConnections.mailboxes, l, myNodeID)
		newConnections.remoteMailboxes[nodeID] = nodeRemoteMailboxes
		connection := &nodeConnector{
			source:           myNode,
			dest:             nodeDef,
			ClusterLogger:    cluster.ClusterLogger,
			remoteMailboxes:  nodeRemoteMailboxes,
			cluster:          cluster,
			connectionServer: newConnections,
		}
		newConnections.nodeConnectors[nodeID] = connection
		newConnections.Add(connection)
		newConnections.Add(nodeRemoteMailboxes)
	}

	if needListener {
		nl := newNodeListener(myNode, newConnections)
		newConnections.Add(nl)
		newConnections.listener = nl
		nl.remoteMailboxes = newConnections.remoteMailboxes
	}

	return newConnections
}

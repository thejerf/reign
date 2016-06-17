package reign

// In order to make this work:
// * There must be an "address" type that can serialize out as an ID
//   and nothing but an ID, then deserialize as either a local or remote
//   inside the concrete type. Then the GobEncoder and GobDecoder types can
//   correctly manage the mailboxes.

// * send messages across
// * route the sent messages to the target mailboxes
// * implement the policy below
// * Implement the remote backpressure.

// FIXME: To be added to the documentation
// Unlike what you may initially suspect, when sending a message to a remote
// node, you will block until that message has been queued up and is ready
// to send. (That is, you will not block while the message is being *sent*,
// so if you have a large message that may take a moment to send you can
// move on.) This is for the purpose of providing backpressure; if the
// connection to the remote node becomes overloaded, it is desirable for
// things using that connection to be slowed up a bit. This may not be
// the optimal policy, but it's a start. In the case where messages are
// being sent substantially slower than the network can handle, the
// desirable common case, this will amount to no pauses.
//
// we can get this pattern by having the senders send to the node, and
// having them also select on the node's status chan; if that gets closed
// due to failure, we give up. We can also select on a timeout chan.
//
// FIXME: This implies that the backpressure of a node ought to be propagated
// back to the cluster itself. "Sufficiently large" backlogs should be propagated
// to any senders, so they can be slowed down locally.

import (
	"crypto/tls"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/thejerf/reign/internal"
)

const (
	clusterVersion = 1
)

// nodeConnector bundles together all of the information about how to connect
// to a node. The actual connection is a nodeConnection. This runs as a
// supervised servec.
type nodeConnector struct {
	source  *NodeDefinition
	dest    *NodeDefinition
	cluster *Cluster
	cancel  bool
	ClusterLogger

	// this mailbox should never get out to the "rest" of the system.
	remoteMailboxes *remoteMailboxes
	sync.Mutex

	// deliberately retained across restarts
	*mailboxes

	connectionServer *connectionServer
	connection       *nodeConnection

	failOnSSLHandshake     bool
	failOnClusterHandshake bool
}

// this is the literal connection to the node.
type nodeConnection struct {
	conn      net.Conn
	tls       *tls.Conn
	rawOutput io.WriteCloser
	rawInput  io.ReadCloser
	output    *gob.Encoder
	input     *gob.Decoder

	connectionServer *connectionServer
	source           *NodeDefinition
	dest             *NodeDefinition
	ClusterLogger
	*nodeConnector

	failOnSSLHandshake     bool
	failOnClusterHandshake bool
}

func (nc *nodeConnector) String() string {
	return fmt.Sprintf("nodeConnector %d -> %d", nc.source.ID, nc.dest.ID)
}

func (nc *nodeConnector) Serve() {
	nc.Trace("node connection from %d to %d starting serve", nc.source.ID, nc.dest.ID)

	connection, err := nc.connect()
	nc.connection = connection
	if err != nil {
		nc.Error("Could not connect to node %v: %s", nc.dest.ID, err.Error())
		return
	}
	nc.Trace("%d -> %d connected", nc.source.ID, nc.dest.ID)

	// sync with the Stop method, which could conceivably be triggered
	// before we even get here
	nc.Lock()
	if nc.cancel {
		connection.terminate()
	}
	defer connection.terminate()
	nc.Unlock()

	err = connection.sslHandshake()
	if err != nil {
		nc.Error("Could not SSL handshake to node %v: %s", nc.dest.ID, err.Error())
		return
	}
	nc.Trace("%d -> %d ssl handshake successful", nc.source.ID, nc.dest.ID)

	err = connection.clusterHandshake()
	if err != nil {
		nc.Error("Could not perform cluster handshake with node %v: %s", nc.dest.ID, err.Error())
		return
	}
	nc.Trace("%d -> %d cluster handshake successful", nc.source.ID, nc.dest.ID)

	// hook up the connection to the permanent message manager
	nc.remoteMailboxes.setConnection(connection)
	defer nc.remoteMailboxes.unsetConnection(connection)
	// and handle all incoming messages
	connection.handleIncomingMessages()
}

func (nc *nodeConnector) Stop() {
	nc.Lock()
	defer nc.Unlock()

	if nc.connection != nil {
		nc.connection.terminate()
		nc.connection = nil
	} else {
		nc.cancel = true
	}
}

func (nc *nodeConnection) terminate() {
	if nc == nil {
		return
	}

	tls := nc.tls
	if tls != nil {
		tls.Close()
	}
	conn := nc.conn
	if conn != nil {
		conn.Close()
	}
	rawOutput := nc.rawOutput
	if rawOutput != nil {
		rawOutput.Close()
	}
	rawInput := nc.rawInput
	if rawInput != nil {
		rawInput.Close()
	}
}

// This establishes a connection to the target node. It does NOTHING ELSE,
// no SSL handshake, nothing.
//
// FIXME: Test that a node definition can't establish two connections to
// the same node.
func (nc *nodeConnector) connect() (*nodeConnection, error) {
	conn, err := net.DialTCP("tcp", nc.source.localaddr, nc.dest.ipaddr)
	if err != nil {
		return nil, err
	}

	return &nodeConnection{
		conn:                   conn,
		rawOutput:              conn,
		rawInput:               conn,
		source:                 nc.source,
		dest:                   nc.dest,
		ClusterLogger:          nc.ClusterLogger,
		connectionServer:       nc.connectionServer,
		failOnSSLHandshake:     nc.failOnSSLHandshake,
		failOnClusterHandshake: nc.failOnClusterHandshake,
		nodeConnector:          nc,
	}, nil
}

func (nc *nodeConnection) sslHandshake() error {
	nc.Trace("Conn to %d in sslHandshake", nc.dest.ID)
	if nc.failOnSSLHandshake {
		nc.conn.Close()
		nc.Trace("Failing on ssl handshake, as instructed")
		return errors.New("failing on ssl handshake, as instructed")
	}
	tlsConfig := nc.connectionServer.Cluster.tlsConfig(nc.dest.ID)
	tlsConn := tls.Client(nc.conn, tlsConfig)

	// I like to run this manually, despite the fact this is run
	// automatically at first communication,, so I get any errors it may
	// produce at a controlled time.
	nc.Trace("Conn to %d handshaking", nc.dest.ID)
	err := tlsConn.Handshake()
	nc.Trace("Conn to %d handshook, err: %#v", nc.dest.ID, err)
	if err != nil {
		return err
	}

	nc.tls = tlsConn

	// Initially, we unconditionally use the TLS connection
	nc.output = gob.NewEncoder(nc.tls)
	nc.input = gob.NewDecoder(nc.tls)
	return nil
}

func (nc *nodeConnection) clusterHandshake() error {
	if nc.failOnClusterHandshake {
		nc.tls.Close()
		nc.Trace("Failing on cluster handshake, as instructed")
		return errors.New("failing on cluster handshake, as instructed")
	}
	handshake := internal.ClusterHandshake{
		ClusterVersion: clusterVersion,
		MyNodeID:       internal.IntNodeID(nc.source.ID),
		YourNodeID:     internal.IntNodeID(nc.dest.ID),
	}

	nc.output.Encode(handshake)

	var serverHandshake internal.ClusterHandshake
	err := nc.input.Decode(&serverHandshake)
	if err != nil {
		return err
	}

	myNodeID := NodeID(serverHandshake.MyNodeID)
	yourNodeID := NodeID(serverHandshake.YourNodeID)
	cs := nc.connectionServer

	if serverHandshake.ClusterVersion != clusterVersion {
		cs.Warn("Remote node id %v claimed unknown cluster version %v, proceeding in the hope that this will all just work out somehow...", nc.dest.ID, serverHandshake.ClusterVersion)
	}
	if myNodeID != nc.dest.ID {
		cs.Warn("The node I thought was #%v is claiming to be #%v instead. These two nodes can not communicate properly. Standing by, hoping a new node definition will resolve this shortly....", nc.dest.ID, serverHandshake.MyNodeID)
	}
	if yourNodeID != nc.source.ID {
		cs.Warn("The node #%v thinks I'm node #%v, but I think I'm node #%v. These two nodes can not communicate properly. Standing by, hoping a new node definition will resolve this shortly...", nc.dest.ID, serverHandshake.YourNodeID, nc.source.ID)
	}

	nc.input = gob.NewDecoder(nc.tls)

	return nil
}

func (nc *nodeConnection) handleIncomingMessages() {
	var cm internal.ClusterMessage
	var err error

	nc.Trace("Connection %d -> %d in handleIncomingMessages", nc.source.ID, nc.dest.ID)

	for err == nil {
		// FIXME: Set read timeouts, to handle network errors
		// FIXME: Send pings, to avoid triggering read timeouts under normal
		// circumstances
		err = nc.input.Decode(&cm)
		if err == nil {
			err = nc.nodeConnector.remoteMailboxes.Send(cm)
			if err != nil {
				nc.Error("Error handling message %#v:\n%#v", cm, err)
			}
		} else {
			panic(fmt.Sprintf("Error decoding message: %s", err.Error()))
		}
	}
}

func (nc *nodeConnection) send(value *internal.ClusterMessage) error {
	// If we are not currently connected, silently eat the message.
	// FIXME: Compare with Erlang.
	if nc == nil {
		return errors.New("no current connection")
	}

	// FIXME: Send timeout
	return nc.output.Encode(value)
}

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
	"time"

	"github.com/thejerf/reign/internal"
)

const (
	clusterVersion = 1
)

// ErrFailOnClusterHandshake is the error returned when a node connector's
// failOnClusterHandshake property is true.
var ErrFailOnClusterHandshake = errors.New("Failing on ssl handshake, as instructed")

// nodeConnector bundles together all of the information about how to connect
// to a node. The actual connection is a nodeConnection. This runs as a
// supervised service.
type nodeConnector struct {
	source  *NodeDefinition
	dest    *NodeDefinition
	cluster *Cluster
	ClusterLogger

	// this mailbox should never get out to the "rest" of the system.
	remoteMailboxes *remoteMailboxes
	sync.Mutex

	// deliberately retained across restarts
	*mailboxes

	connectionServer *connectionServer
	connection       *nodeConnection

	cancel bool

	failOnSSLHandshake     bool
	failOnClusterHandshake bool
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

func (nc *nodeConnector) String() string {
	// Since the node connector's Serve() method acquires a lock while determining
	// if it should cancel, we need to make sure that we also acquire that lock before
	// replying to Suture's service name inquiry.
	nc.Lock()
	defer nc.Unlock()

	return fmt.Sprintf("nodeConnector %d -> %d", nc.source.ID, nc.dest.ID)
}

func (nc *nodeConnector) Serve() {
	nc.Infof("node connection from %d to %d, starting serve", nc.source.ID, nc.dest.ID)

	connection, err := nc.connect()
	nc.connection = connection
	if err != nil {
		nc.Errorf("Could not connect to node %v: %s", nc.dest.ID, err.Error())
		return
	}
	nc.Infof("%d -> %d connected", nc.source.ID, nc.dest.ID)

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
		nc.Errorf("Could not SSL handshake to node %v: %s", nc.dest.ID, err.Error())
		return
	}
	nc.Infof("%d -> %d ssl handshake successful", nc.source.ID, nc.dest.ID)

	err = connection.clusterHandshake()
	if err != nil {
		nc.Errorf("Could not perform cluster handshake with node %v: %s", nc.dest.ID, err.Error())
		return
	}
	nc.Infof("%d -> %d cluster handshake successful", nc.source.ID, nc.dest.ID)

	// Synchronize registry with the remote node.
	err = connection.registrySync()
	if err != nil {
		nc.Errorf("Could not sync registry with node %v: %s", nc.dest.ID, err.Error())
		return
	}
	nc.Infof("%d -> %d registry sync successful", nc.source.ID, nc.dest.ID)

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

// this is the literal connection to the node.
type nodeConnection struct {
	output         *gob.Encoder
	input          *gob.Decoder
	resetPingTimer chan time.Duration

	connectionServer *connectionServer
	source           *NodeDefinition
	dest             *NodeDefinition
	ClusterLogger
	*nodeConnector

	failOnSSLHandshake     bool
	failOnClusterHandshake bool

	// Used for testing purposes to peek in on incoming messages.
	peekFunc func(internal.ClusterMessage)

	conn      net.Conn
	tls       *tls.Conn
	rawOutput io.WriteCloser
	rawInput  io.ReadCloser
}

// peekIncomingMessage accepts a cluster message and sends it to the
// peekFunc(), if set.
func (nc *nodeConnection) peekIncomingMessage(cm internal.ClusterMessage) {
	nc.Lock()
	defer nc.Unlock()

	if nc.peekFunc == nil {
		return
	}

	nc.peekFunc(cm)
}

// setPeekFunc accepts a function and assigns it to the node connection's
// peekFunc field.
func (nc *nodeConnection) setPeekFunc(pf func(internal.ClusterMessage)) {
	nc.Lock()
	defer nc.Unlock()

	nc.peekFunc = pf
}

// resetConnectionDeadline resets the network connection's deadline to
// the specified duration from time.Now().
func (nc *nodeConnection) resetConnectionDeadline(d time.Duration) {
	err := nc.conn.SetDeadline(time.Now().Add(d))
	if err != nil {
		nc.Errorf("Unable to set network connection deadline: %s", err)
	}
}

func (nc *nodeConnection) terminate() {
	if nc == nil {
		return
	}

	if nc.tls != nil {
		_ = nc.tls.Close()
	}

	if nc.conn != nil {
		_ = nc.conn.Close()
	}

	if nc.rawOutput != nil {
		_ = nc.rawOutput.Close()
	}

	if nc.rawInput != nil {
		_ = nc.rawInput.Close()
	}
}

func (nc *nodeConnection) sslHandshake() error {
	nc.Tracef("Conn to %d in sslHandshake", nc.dest.ID)
	if nc.failOnSSLHandshake {
		nc.terminate()

		return ErrFailOnClusterHandshake
	}

	tlsConfig := nc.connectionServer.Cluster.tlsConfig(nc.dest.ID)
	tlsConn := tls.Client(nc.conn, tlsConfig)

	// I like to run this manually, despite the fact this is run
	// automatically at first communication,, so I get any errors it may
	// produce at a controlled time.
	nc.Tracef("Conn to %d handshaking", nc.dest.ID)
	err := tlsConn.Handshake()
	if err != nil {
		return err
	}

	nc.tls = tlsConn
	nc.output = gob.NewEncoder(nc.tls)
	nc.input = gob.NewDecoder(nc.tls)

	return nil
}

func (nc *nodeConnection) clusterHandshake() error {
	if nc.failOnClusterHandshake {
		nc.terminate()

		return ErrFailOnClusterHandshake
	}
	handshake := internal.ClusterHandshake{
		ClusterVersion: clusterVersion,
		MyNodeID:       internal.IntNodeID(nc.source.ID),
		YourNodeID:     internal.IntNodeID(nc.dest.ID),
	}
	err := nc.output.Encode(handshake)
	if err != nil {
		return err
	}

	var serverHandshake internal.ClusterHandshake
	err = nc.input.Decode(&serverHandshake)
	if err != nil {
		return err
	}

	myNodeID := NodeID(serverHandshake.MyNodeID)
	yourNodeID := NodeID(serverHandshake.YourNodeID)

	if serverHandshake.ClusterVersion != clusterVersion {
		connections.Warnf("Remote node id %v claimed unknown cluster version %v, proceeding in the hope that this will all just work out somehow...",
			nc.dest.ID, serverHandshake.ClusterVersion)
	}
	if myNodeID != nc.dest.ID {
		connections.Warnf("The node I thought was #%v is claiming to be #%v instead. These two nodes can not communicate properly. Standing by, hoping a new node definition will resolve this shortly....",
			nc.dest.ID, serverHandshake.MyNodeID)
	}
	if yourNodeID != nc.source.ID {
		connections.Warnf("The node #%v thinks I'm node #%v, but I think I'm node #%v. These two nodes can not communicate properly. Standing by, hoping a new node definition will resolve this shortly...",
			nc.dest.ID, serverHandshake.YourNodeID, nc.source.ID)
	}

	nc.input = gob.NewDecoder(nc.tls)

	return nil
}

// registrySync sends this node's registry MailboxID to the remote node.
func (nc *nodeConnection) registrySync() error {
	// Send our registry synchronization data to the remote node.
	rs := internal.RegisterRemoteNode{
		Node:      internal.IntNodeID(nc.source.ID),
		MailboxID: internal.IntMailboxID(nc.connectionServer.registry.Address.GetID()),
	}
	err := nc.output.Encode(rs)
	if err != nil {
		return err
	}

	// Receive the remote node's registry synchronization data.
	var irs internal.RegisterRemoteNode
	err = nc.input.Decode(&irs)
	if err != nil {
		return err
	}

	nc.Tracef("Received mailbox ID %x from node %d", irs.MailboxID, irs.Node)

	// Add remote node's registry mailbox ID to the nodeRegistries map.  We need
	// to register this *before* we generate and send our registry claims else
	// some registry changes won't sync.
	nc.connectionServer.registry.addNodeRegistry(
		NodeID(irs.Node),
		Address{
			mailboxID:        MailboxID(irs.MailboxID),
			connectionServer: nc.connectionServer,
		},
	)

	return nil
}

// handleIncomingMessages handle replies from the remote node after this
// node makes a successful connection.
func (nc *nodeConnection) handleIncomingMessages() {
	var (
		cm   internal.ClusterMessage
		err  error
		pong internal.ClusterMessage = internal.Pong{}
	)

	// Report the successful connection, and defer the disconnection status change call.
	nc.connectionServer.changeConnectionStatus(nc.dest.ID, true)
	defer nc.connectionServer.changeConnectionStatus(nc.dest.ID, false)

	nc.resetPingTimer = make(chan time.Duration)
	done := make(chan struct{})
	defer func() {
		close(nc.resetPingTimer)
		close(done)
	}()

	go pingRemote(nc.output, nc.resetPingTimer, nc.ClusterLogger)

	nc.Tracef("Connection %d -> %d in handleIncomingMessages", nc.source.ID, nc.dest.ID)
	nc.resetConnectionDeadline(DeadlineInterval)

	// Send our registry claims.
	var claims internal.ClusterMessage = nc.connectionServer.registry.getAllNodeClaims()
	err = nc.output.Encode(&claims)
	if err != nil {
		nc.Errorf("Sending registry claims: %s", err)
	}

	for err == nil {
		err = nc.input.Decode(&cm)
		switch err {
		case nil:
			// We received a message.  No need to PING the remote node.
			nc.resetPingTimer <- DefaultPingInterval

			nc.peekIncomingMessage(cm)

			switch msg := cm.(type) {
			case *internal.AllNodeClaims:
				nc.Tracef("Received claims from node %d", msg.Node)
				_ = nc.connectionServer.registry.Send(msg)
			case *internal.Ping:
				err = nc.output.Encode(&pong)
				if err != nil {
					nc.Errorf("Attempted to pong remote node: %s", err)
				}
			case *internal.Pong:
			default:
				err = nc.nodeConnector.remoteMailboxes.Send(cm)
				if err != nil {
					nc.Errorf("Error handling message %#v:\n%#v", cm, err)
				}
			}
			nc.resetConnectionDeadline(DeadlineInterval)
		case io.EOF:
			nc.Errorf("Connection to node ID %v has gone down", nc.dest.ID)
		default:
			nErr, ok := err.(net.Error)
			if ok && nErr.Temporary() {
				// The pinger is monitoring temporary errors and will panic if
				// its threshold is exceeded.
				nc.Warn(err)
				err = nil
			} else {
				nc.Errorf("Error decoding message: %s", err)
			}
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

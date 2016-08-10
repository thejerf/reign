package reign

import (
	"crypto/tls"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/thejerf/reign/internal"
)

// PingInterval determines the minimum interval between PING messages.
// Defaults to 30 seconds.
var PingInterval = time.Second * 30

// DeadlineInterval determine how long to keep the network connection open after
// a successful read.  The net.Conn deadline value will be reset to time.Now().Add(DeadlineInterval)
// upon each successful message read over the network.  Defaults to 5 minutes.
var DeadlineInterval = time.Minute * 5

// This file defines the listener, which listens for incoming node connections,
// and the handler that the listeners run.

type nodeListener struct {
	node             *NodeDefinition
	connectionServer *connectionServer
	listener         net.Listener
	stopped          bool
	ClusterLogger

	// Once constructed by connection.go, this map is read-only, so no sync
	// is necessary.
	remoteMailboxes map[NodeID]*remoteMailboxes
	sync.Mutex
	condition *sync.Cond

	// test criteria
	failOnSSLHandshake     bool
	failOnClusterHandshake bool
}

func newNodeListener(node *NodeDefinition, connectionServer *connectionServer) *nodeListener {
	nl := &nodeListener{node: node, connectionServer: connectionServer,
		ClusterLogger: connectionServer.ClusterLogger}
	nl.condition = sync.NewCond(&nl.Mutex)
	return nl
}

func (nl *nodeListener) mailboxesForNode(id NodeID) *remoteMailboxes {
	mailboxes, exists := nl.remoteMailboxes[id]
	if !exists {
		panic(fmt.Sprintf("Can't get mailbox for node %d, %#v", id, nl.remoteMailboxes))
	}
	return mailboxes
}

// This is used by the tests, which simulate multiple nodes within one
// process. Trying to bring up multiple cluster nodes simultaneously
// causes race conditions with trying to connect before the listener
// is running, which slows the tests down. This allows us to wait for a
// listener to complete its setup before we bring up the next node.
func (nl *nodeListener) waitForListen() {
	// a nil node listener on connectionServer specifically indicates
	// that we're not listening at all.
	if nl == nil {
		return
	}
	nl.Trace("Waiting for listener")
	nl.Lock()
	for nl.listener == nil {
		nl.condition.Wait()
	}
	nl.Unlock()
	nl.Trace("Listener successfully waited for")
}

func (nl *nodeListener) String() string {
	// Since the node listener's Serve() method acquires a lock while setting
	// its listener, we need to make sure that we also acquire that lock before
	// replying to Suture's service name inquiry.
	nl.Lock()
	defer nl.Unlock()

	return fmt.Sprintf("nodeListener %d on %s", nl.node.ID, nl.node.Address)
}

func (nl *nodeListener) Serve() {
	// This locks for the duration of a net.ListenTCP call. I *think* we can
	// assume this is a pretty fast call; either the OS gives it to you or
	// it doesn't. If so, I think this is all safe with .Stop(). If not,
	// it's possible the suture.Supervisor will decide this took too long
	// to shut down... and if that happens, the mutex just locks up entirely
	// until the first Listen clears.
	nl.Lock()

	// The only way this gets set to true is if Stop was called before we
	// even got here. Reset the stopped flag to false and exit.
	if nl.stopped {
		nl.stopped = false
		nl.Unlock()
		return
	}

	nl.listener = nil

	if nl.node.listenaddr == nil {
		nl.Unlock()
		panic(fmt.Sprintf("Can't start listener for node %d because we have no ListenAddress", nl.node.ID))
	}

	listener, err := net.ListenTCP("tcp", nl.node.listenaddr)
	if err != nil {
		nl.Unlock()
		panic(fmt.Sprintf("Can't start listener on node %d, because while trying to listen, we got: %s", nl.node.ID, err.Error()))
	}

	nl.listener = listener
	nl.Unlock()

	nl.condition.Broadcast()

	for {
		conn, err := nl.listener.Accept()
		if err != nil {
			if nl.stopped {
				return
			}

			nl.Error("Lost listener for cluster: %s", myString(err))
			return
		}

		from := conn.RemoteAddr().String()
		nl.Info("Cluster connection received from %s", from)

		incoming := &incomingConnection{nodeListener: nl, tcpConn: conn,
			conn: conn, server: nl.node}
		go incoming.handleConnection()
	}
}

// FIXME: When it's more clear what's going on, collapse this with nodeConnection
// as there's currently a lot of duplication here
type incomingConnection struct {
	*nodeListener
	remoteMailboxes *remoteMailboxes

	output *gob.Encoder
	input  *gob.Decoder

	client *NodeDefinition
	server *NodeDefinition

	conn      net.Conn // The connection we are actually using to send data
	tcpConn   net.Conn // The raw TCP connection, no matter what we're doing
	tls       net.Conn // The TLS connection, if any
	pingTimer *time.Timer
}

// resetConnectionDeadline resets the network connection's deadline to
// the specified duration from time.Now().
func (ic *incomingConnection) resetConnectionDeadline(d time.Duration) {
	err := ic.conn.SetDeadline(time.Now().Add(d))
	if err != nil {
		ic.Error("Unable to set network connection deadline: %s", err)
	}
}

// resetPingTimer accepts a duration and resets the incoming connection's
// ping timer.  The timer is initialized if it wasn't previously set.
func (ic *incomingConnection) resetPingTimer(d time.Duration) {
	ic.Lock()
	defer ic.Unlock()

	if ic.pingTimer == nil {
		ic.pingTimer = time.NewTimer(d)

		return
	}

	ic.pingTimer.Reset(d)
}

func (ic *incomingConnection) send(value *internal.ClusterMessage) error {
	if ic == nil {
		return errors.New("no current connection")
	}

	// FIXME Send timeout
	err := ic.output.Encode(value)
	return err
}

func (ic *incomingConnection) terminate() {
	tls := ic.tls
	if tls != nil {
		tls.Close()
	}
	tcpConn := ic.tcpConn
	if tcpConn != nil {
		tcpConn.Close()
	}
	return
}

func myString(i interface{}) string {
	switch m := i.(type) {
	case error:
		return m.Error()
	case fmt.Stringer:
		return m.String()
	default:
		return fmt.Sprintf("%#v", i)
	}
}

func (ic *incomingConnection) handleConnection() {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 1000*1000)
			l := runtime.Stack(buf, false)
			ic.Error("Listener handler crashed: %s\n%s", myString(r), string(buf[:l]))
			if ic.tls != nil {
				ic.tls.Close()
			}
			if ic.tcpConn != nil {
				ic.tcpConn.Close()
			}
		}
	}()

	ic.Trace("Listener for %d got connection", ic.server.ID)
	err := ic.sslHandshake()
	if err != nil {
		// FIXME: This ought to wrap the error somehow, not smash to string,
		// which would frankly be hypocritical
		panic("Could not SSL handshake the incoming connection: " + err.Error())
	}
	ic.Trace("Listener for %d successfully SSL'ed", ic.server.ID)

	err = ic.clusterHandshake()
	if err != nil {
		ic.Error("Could not cluster handshake the incoming connection: " + err.Error())
		return
	}
	ic.Trace("Listener for %d successfully cluster handshook", ic.server.ID)

	ic.remoteMailboxes = ic.mailboxesForNode(ic.client.ID)
	ic.remoteMailboxes.setConnection(ic)
	defer ic.remoteMailboxes.unsetConnection(ic)

	// Sync registry mailbox ID with the remote node.
	err = ic.registryMailboxSync()
	if err != nil {
		ic.Error("Could not sync registry mailbox IDs with node %v: %s", ic.client.ID, err.Error())
		return
	}
	ic.Trace("Listener for %d successfully synced registry mailbox", ic.server.ID)

	// TODO: Synchronize remoteMailboxes between nodes.

	ic.handleIncomingMessages()
}

// FIXME: This ought to be refactored with the node
func (ic *incomingConnection) sslHandshake() error {
	ic.Trace("Listener for %d in sslHandshake", ic.server.ID)
	// FIXME: Demeter is yelling at me here.
	if ic.nodeListener.failOnSSLHandshake {
		ic.Trace("But I've been told to fail the handshake hard")
		ic.terminate()
		return errors.New("ssl handshake simulating failure")
	}
	tlsConfig := ic.nodeListener.connectionServer.Cluster.tlsConfig(ic.server.ID)
	tls := tls.Server(ic.conn, tlsConfig)
	ic.Trace("Listener for %d made the tlsConn, handshaking", ic.server.ID)

	err := tls.Handshake()
	if err != nil {
		ic.Trace("Listener for %d handshook err: %s", ic.server.ID, myString(err))
		return err
	}

	ic.tls = tls
	ic.conn = tls
	ic.output = gob.NewEncoder(ic.conn)
	ic.input = gob.NewDecoder(ic.conn)

	return nil
}

// FIXME: Eliminate localNode in favor of the Cluster's ThisNode field

func (ic *incomingConnection) clusterHandshake() error {
	if ic.nodeListener.failOnClusterHandshake {
		ic.Trace("In cluster handshake, and told to fail it")
		ic.terminate()
		return errors.New("cluster handshake simulating failure")
	}
	var clientHandshake internal.ClusterHandshake
	err := ic.input.Decode(&clientHandshake)
	if err != nil {
		return err
	}

	myNodeID := NodeID(clientHandshake.MyNodeID)
	yourNodeID := NodeID(clientHandshake.YourNodeID)

	if clientHandshake.ClusterVersion != clusterVersion {
		ic.Warn("Remote node %d claimed unknown cluster version %v, proceding in the hope that this will all just work out somehow...",
			clientHandshake.MyNodeID, clientHandshake.ClusterVersion)
	}
	if yourNodeID != ic.nodeListener.connectionServer.Cluster.ThisNode.ID {
		ic.Warn("The remote node (claiming ID %d) thinks I'm node %d, but I think I'm node %d. These two nodes can not communicate properly. Standing by, hoping a new node definition will resolve this shortly...",
			clientHandshake.MyNodeID, clientHandshake.YourNodeID, ic.nodeListener.connectionServer.Cluster.ThisNode.ID)
	}

	clientNodeDefinition, exists := ic.nodeListener.connectionServer.Cluster.Nodes[myNodeID]
	if !exists {
		ic.Error("Connecting node claims to have ID %d, but I don't have a definition for that node.", clientHandshake.MyNodeID)
	}
	ic.client = clientNodeDefinition

	myHandshake := internal.ClusterHandshake{
		ClusterVersion: clusterVersion,
		MyNodeID:       internal.IntNodeID(ic.nodeListener.connectionServer.Cluster.ThisNode.ID),
		YourNodeID:     clientHandshake.MyNodeID,
	}
	ic.output.Encode(myHandshake)

	ic.input = gob.NewDecoder(ic.tls)

	return nil
}

func (ic *incomingConnection) registryMailboxSync() error {
	rm := internal.RegistryMailbox{
		Node:      internal.IntNodeID(ic.node.ID),
		MailboxID: internal.IntMailboxID(ic.connectionServer.registry.Address.GetID()),
	}

	err := ic.output.Encode(rm)
	if err != nil {
		return err
	}

	var irm internal.RegistryMailbox
	err = ic.input.Decode(&irm)
	if err != nil {
		return err
	}

	ic.Trace("Received mailbox ID %x from node %d", irm.MailboxID, irm.Node)

	if ic.connectionServer.registry.nodeRegistries == nil {
		ic.connectionServer.registry.nodeRegistries = make(map[NodeID]Address)
	}

	ic.connectionServer.registry.nodeRegistries[NodeID(irm.Node)] = Address{
		mailboxID:        MailboxID(irm.MailboxID),
		connectionServer: ic.connectionServer,
	}

	return nil
}

// handleIncomingMessages handles messages from the remote nodes after
// making a successful connection through this node's listener.
func (ic *incomingConnection) handleIncomingMessages() {
	var (
		cm   internal.ClusterMessage
		err  error
		ping internal.ClusterMessage = internal.Ping{}
		pong internal.ClusterMessage = internal.Pong{}
		done                         = make(chan struct{})
	)
	defer close(done)

	ic.resetConnectionDeadline(DeadlineInterval)

	go func() {
		// Send PING messages to the remote node at regular intervals.
		// The pingTimer may never fire if messages come in more frequently
		// than the PingInterval.
		ic.resetPingTimer(PingInterval)

		for {
			select {
			case <-ic.pingTimer.C:
				err = ic.output.Encode(&ping)
				if err != nil {
					ic.Error("Attempted to ping node %d: %s", ic.client.ID, err)
				}
				ic.resetPingTimer(PingInterval)
			case <-done:
				return
			}
		}
	}()

	for err == nil {
		err = ic.input.Decode(&cm)
		if err == nil {
			// We received a message.  No need to PING the remote node.
			ic.resetPingTimer(PingInterval)

			switch cm.(type) {
			case *internal.Ping:
				err = ic.output.Encode(&pong)
				if err != nil {
					ic.Error("Attempted to pong node %d: %s", ic.client.ID, err)
				}
			case *internal.Pong:
			default:
				err = ic.remoteMailboxes.Send(cm)
				if err != nil {
					ic.Error("Error handling message %#v:\n%#v", cm, err)
				}
			}
			ic.resetConnectionDeadline(DeadlineInterval)
		} else if err == io.EOF {
			ic.Error("Connection to node ID %v has gone down", ic.client.ID)
		} else {
			panic(fmt.Sprintf("Error decoding message: %s", err.Error()))
		}
	}
}

func (nl *nodeListener) Stop() {
	nl.Lock()
	defer nl.Unlock()

	if nl.listener != nil {
		nl.stopped = true
		nl.listener.Close()
	} else {
		nl.stopped = true
	}
}

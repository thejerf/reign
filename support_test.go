package reign

import (
	"fmt"
	"runtime"
	"time"
)

// timeout is the duration used for ReceiveNextTimeout() calls.
var timeout = time.Second

// This file contains code that supports the tests, including:
// * Creating various clusters and starting them up in various ways
// * A function that returns whether a chunk of code panics.

// This defines a "network test bed", which is two nodes that connect
// to each other, have two mailboxes each, and remotely-bound mailboxes
// for easily testing.
//
// With the design of reign, technically the whole "cluster" is really
// just all the 1-to-1 connections aggregated together, so the vast
// majority of the test suite is just testing two nodes connected together.
// (Possibly the whole thing, but we'll see.)
type NetworkTestBed struct {
	node1connectionServer *connectionServer
	node2connectionServer *connectionServer

	node1mailbox1 *Mailbox
	node1address1 *Address
	node1mailbox2 *Mailbox
	node1address2 *Address

	node2mailbox1 *Mailbox
	node2address1 *Address
	node2mailbox2 *Mailbox
	node2address2 *Address

	// These "remote" addresses are all bound to the address indicated
	// by their suffix, from the point of view of the "other" node, so
	// fromNode2toNode1mailbox1 indicates the node 1's mailbox 1 from the
	// point of view of node 2.
	fromNode2toNode1mailbox1 *Address
	fromNode2toNode1mailbox2 *Address
	fromNode1toNode2mailbox1 *Address
	fromNode1toNode2mailbox2 *Address

	node1remoteMailboxes *remoteMailboxes
	node2remoteMailboxes *remoteMailboxes
}

func (ntb *NetworkTestBed) terminateServers() {
	ntb.node1connectionServer.Stop()
	ntb.node2connectionServer.Stop()
}

func (ntb *NetworkTestBed) terminateMailboxes() {
	ntb.node1mailbox1.Terminate()
	ntb.node1mailbox2.Terminate()
	ntb.node2mailbox1.Terminate()
	ntb.node2mailbox2.Terminate()
	ntb.node1connectionServer.Terminate()
	ntb.node2connectionServer.Terminate()
}

func (ntb *NetworkTestBed) terminate() {
	if r := recover(); r != nil {
		fmt.Println("Error in test:", r)
		buf := make([]byte, 100000)
		n := runtime.Stack(buf, false)
		fmt.Println("Stack at error:\n", string(buf[:n]))
	}

	ntb.terminateServers()
	ntb.terminateMailboxes()
}

func (ntb *NetworkTestBed) with1(f func()) {
	connectionsL.Lock()
	defer connectionsL.Unlock()
	connections = ntb.node1connectionServer
	f()
}

func (ntb *NetworkTestBed) with2(f func()) {
	connectionsL.Lock()
	defer connectionsL.Unlock()
	connections = ntb.node2connectionServer
	f()
}

func testSpec() *ClusterSpec {
	return &ClusterSpec{
		Nodes: []*NodeDefinition{
			{ID: NodeID(1), Address: "127.0.0.1:29876"},
			{ID: NodeID(2), Address: "127.0.0.1:29877"},
		},
		PermittedProtocols: []string{"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"},
		ClusterCertPEM:     string(signing1Cert),
	}
}

func unstartedTestbed(spec *ClusterSpec, l ClusterLogger) *NetworkTestBed {
	if spec == nil {
		spec = testSpec()
	}

	if l == nil {
		l = NullLogger
	}

	ntb := &NetworkTestBed{}

	var err error

	spec.NodeKeyPEM = string(node2_1Key)
	spec.NodeCertPEM = string(node2_1Cert)
	setConnections(nil)
	ntb.node2connectionServer, _, err = createFromSpec(spec, 2, l)
	if err != nil {
		panic(err)
	}
	setConnections(nil)

	spec.NodeKeyPEM = string(node1_1Key)
	spec.NodeCertPEM = string(node1_1Cert)
	ntb.node1connectionServer, _, err = createFromSpec(spec, 1, l)
	if err != nil {
		panic(err)
	}

	setConnections(ntb.node1connectionServer)
	ntb.node1address1, ntb.node1mailbox1 = connections.NewMailbox()
	ntb.node1address1.connectionServer = ntb.node1connectionServer
	ntb.node1address2, ntb.node1mailbox2 = connections.NewMailbox()
	ntb.node1address2.connectionServer = ntb.node1connectionServer

	setConnections(ntb.node2connectionServer)
	ntb.node2address1, ntb.node2mailbox1 = connections.NewMailbox()
	ntb.node2address1.connectionServer = ntb.node2connectionServer
	ntb.node2address2, ntb.node2mailbox2 = connections.NewMailbox()
	ntb.node2address2.connectionServer = ntb.node2connectionServer

	ntb.fromNode2toNode1mailbox2 = &Address{
		mailboxID:        ntb.node2address1.mailboxID,
		connectionServer: ntb.node1connectionServer,
	}
	ntb.fromNode1toNode2mailbox2 = &Address{
		mailboxID:        ntb.node2address2.mailboxID,
		connectionServer: ntb.node1connectionServer,
	}

	ntb.fromNode2toNode1mailbox1 = &Address{
		mailboxID:        ntb.node1address1.mailboxID,
		connectionServer: ntb.node2connectionServer,
	}
	ntb.fromNode1toNode2mailbox1 = &Address{
		mailboxID:        ntb.node1address2.mailboxID,
		connectionServer: ntb.node2connectionServer,
	}

	ntb.node1remoteMailboxes = ntb.node1connectionServer.remoteMailboxes[2]
	ntb.node2remoteMailboxes = ntb.node2connectionServer.remoteMailboxes[1]

	setConnections(nil)

	return ntb
}

func testbed(spec *ClusterSpec, l ClusterLogger) *NetworkTestBed {
	if spec == nil {
		spec = testSpec()
	}
	ntb := unstartedTestbed(spec, l)

	go ntb.node2connectionServer.Serve()
	ntb.node2connectionServer.waitForListen()
	go ntb.node1connectionServer.Serve()

	ntb.node1connectionServer.waitForConnection(NodeID(2))
	ntb.node2connectionServer.waitForConnection(NodeID(1))

	return ntb
}

func panics(f func()) (panics bool) {
	defer func() {
		if r := recover(); r != nil {
			panics = true
		}
	}()

	f()

	return
}

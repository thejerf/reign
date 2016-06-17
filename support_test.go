package reign

import (
	"fmt"
	"runtime"
)

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
	c1 *connectionServer
	c2 *connectionServer

	mailbox1_1 *Mailbox
	addr1_1    Address
	mailbox2_1 *Mailbox
	addr2_1    Address

	mailbox1_2 *Mailbox
	addr1_2    Address
	mailbox2_2 *Mailbox
	addr2_2    Address

	// These "remote" addresses are all bound to the address indicated
	// by their suffix, from the point of view of the "other" node, so
	// rem1_1 indicates the 1_1 Mailbox from the point of view of node 2.
	rem1_1 Address
	rem1_2 Address
	rem2_1 Address
	rem2_2 Address

	remote1to2 *remoteMailboxes
	remote2to1 *remoteMailboxes
}

func (ntb *NetworkTestBed) terminateServers() {
	ntb.c1.Stop()
	ntb.c2.Stop()
}

func (ntb *NetworkTestBed) terminateMailboxes() {
	ntb.mailbox1_1.Terminate()
	ntb.mailbox2_1.Terminate()
	ntb.mailbox1_2.Terminate()
	ntb.mailbox2_2.Terminate()
	ntb.c1.Terminate()
	ntb.c2.Terminate()
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

func testSpec() *ClusterSpec {
	return &ClusterSpec{
		Nodes: map[string]*NodeDefinition{
			"1": {
				Address: "127.0.0.1:29876",
			},
			"2": {
				Address: "127.0.0.1:29877",
			},
		},
		ClusterCertPEM: string(signing1_cert),
	}
}

func unstartedTestbed(spec *ClusterSpec) *NetworkTestBed {
	if spec == nil {
		spec = testSpec()
	}

	ntb := &NetworkTestBed{}

	var err error

	spec.NodeKeyPEM = string(node2_1_key)
	spec.NodeCertPEM = string(node2_1_cert)
	ntb.c2, _, err = createFromSpec(spec, 2, NullLogger)
	if err != nil {
		panic(err)
	}

	spec.NodeKeyPEM = string(node1_1_key)
	spec.NodeCertPEM = string(node1_1_cert)
	ntb.c1, _, err = createFromSpec(spec, 1, NullLogger)
	if err != nil {
		panic(err)
	}

	ntb.addr1_1, ntb.mailbox1_1 = ntb.c1.NewMailbox()
	ntb.addr1_1.connectionServer = ntb.c1
	ntb.addr2_1, ntb.mailbox2_1 = ntb.c1.NewMailbox()
	ntb.addr2_1.connectionServer = ntb.c1

	ntb.addr1_2, ntb.mailbox1_2 = ntb.c2.NewMailbox()
	ntb.addr1_2.connectionServer = ntb.c2
	ntb.addr2_2, ntb.mailbox2_2 = ntb.c2.NewMailbox()
	ntb.addr2_2.connectionServer = ntb.c2

	ntb.rem1_2 = Address{ntb.addr1_2.id, ntb.c1, nil}
	ntb.rem2_2 = Address{ntb.addr2_2.id, ntb.c1, nil}

	ntb.rem1_1 = Address{ntb.addr1_1.id, ntb.c2, nil}
	ntb.rem2_1 = Address{ntb.addr2_1.id, ntb.c2, nil}

	ntb.remote1to2 = ntb.c1.remoteMailboxes[2]
	ntb.remote2to1 = ntb.c2.remoteMailboxes[1]

	return ntb
}

func testbed(spec *ClusterSpec) *NetworkTestBed {
	if spec == nil {
		spec = testSpec()
	}
	ntb := unstartedTestbed(spec)

	go ntb.c2.Serve()
	ntb.c2.waitForListen()
	go ntb.c1.Serve()

	ntb.c1.waitForConnection(NodeID(2))
	ntb.c2.waitForConnection(NodeID(1))

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

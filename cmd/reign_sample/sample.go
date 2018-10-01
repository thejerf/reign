/*

Executable reign_sample contains a simple demonstration of using the
reign library to run a cluster of nodes.

This implements a klunky, lowest-common-denominator terminal chat program
between two nodes. The default configuration will run these two nodes
on localhost, but with some trivial modifications to the sample file it can
be run remotely.

Note that once both nodes are running, it may take a moment for them to
connect as they pass through the backoff algorithm.

*/
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/thejerf/reign"
)

func main() {
	if len(os.Args) < 2 {
		_, _ = fmt.Fprint(os.Stderr,
			"Must pass the node this is going to be (1 or 2) as the "+
				"argument\n")
		os.Exit(1)
	}

	nodeIDStr := os.Args[1]
	nodeInt, err := strconv.Atoi(nodeIDStr)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"Couldn't figure out the passed-in node id: %v\n", err)
		os.Exit(1)
	}

	// Create the cluster from the JSON format. If you have modified the
	// cluster file, you may want to set reign.NullLogger to nil instead,
	// so you'll get logging to STDERR.
	connectionService, names, err := reign.CreateFromSpecFile(
		"sample_config.json", reign.NodeID(nodeInt), reign.NullLogger)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"Couldn't start cluster properly (did you run 'reign_init'?): %v\n",
			err)
		os.Exit(1)
	}

	// start the cluster running. This is designed to integrate with
	// github.com/thejerf/suture to provide Erlang-like supervision
	// for the cluster management, but it is not mandatory. Of course, if
	// you're porting an Erlang code base you'll probably want that too...
	go connectionService.Serve()

	// Register tells the cluster we're going to send this sort of
	// message. (Required because the serialization library requires it.)
	reign.RegisterType(Message{})

	// Create the mailbox. Since a mailbox in Go has two parts, we change
	// the terminology for both to avoid any confusion with
	// Erlang. "address" is like an Erlang PID. "mailbox" is the
	// first-class representation of what receives mail sent to that
	// address.
	address, mailbox := connectionService.NewMailbox()

	// When coding programs at scale, getting names out of the name server
	// is generally an exception. Usually a mailbox address is something
	// that will come from another message, or a database entry, or
	// something like that. But the name service is provided precisely
	// because it is often useful to bootstrap connectivity.
	//
	// Note there is nothing mandatory about the name service; if you work
	// entirely in databases, that's fine.
	myName := fmt.Sprintf("chatter_%d", nodeInt)
	err = names.Register(myName, address)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"Couldn't register name %s: %v", myName, err)
		os.Exit(1)
	}

	targetNode := 1
	if nodeInt == 1 {
		targetNode = 2
	}

	// Write the chat listener:
	go func() {
		for {
			// As the receiver, we use the mailbox:
			msg, err := mailbox.Receive(context.Background())
			if err != nil {
				fmt.Println("ERROR:", err)
			}

			// If your mailbox receives multiple types, you'd normally have
			// to do a type switch here, but in this example, there is only
			// one type:
			fmt.Println("Received:", msg.(Message).Message)
		}
	}()

	// Now we go into the chat loop, for demonstration purposes.
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("Chat message: ")
		text, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("Bye!")
				os.Exit(0)
			}
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			os.Exit(1)
		}

		targetAddr := names.Lookup(fmt.Sprintf("chatter_%d", targetNode))
		if targetAddr == nil {
			fmt.Fprintf(os.Stderr,
				"Couldn't lookup chatter_%d; not running yet?\n",
				targetNode)
			continue
		}

		// See documentation on .Send for what can actually error. Because
		// we happen to *know* this is remote, by construction, we know
		// this can't error.
		//
		// In general, in an Erlang-like environment, you use timeouts when
		// you expect a reply to detect problems. The error from .Send is
		// not quite useless, but lack of error means less than you might
		// think.
		_ = targetAddr.Send(Message{string(text)})
	}
}

// A Message is a message the user typed in.
type Message struct {
	Message string
}

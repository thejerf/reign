/*

Package cluster implements the Erlang-like clustering support.

What Erlang Clustering Is

I found it was easy to misunderstand what "Erlang-like clustering" is when I
first started programming Erlang. Essentially what Erlang gives you is two
things:

 * A PID which can be sent messages from anywhere within a cluster. The
   message itself may include PIDs, which remain live and useful through
   the transfer.
 * Mnesia, a shared dabatase, which can contain PIDs.

It certainly does not give you automatic, trivial clustering or anything
like that. You must still write a clusterable system, which is made much
easier with these primitives, but still non-trivial.

This package provides the former, though I call the PIDs "Address"es,
because in Go they involve neither Processes nor IDentifiers. I don't
bother with the latter, because in the modern day all that is necessary
is to provide for marshalling the addresses to databases, which is
provided, both as text and binary.

Clustering networks together target nodes via TCP to support the
ability to message remote mailboxes with the same syntax that you message
local ones, and the ability to send a local mailbox reference across the
wire such that it can still send messages even from the receiver's side,
allowing node-transparency. It also provides the ability to create named
mailboxes attached to the node, which can be used to bootstrap your
services.

What Reign Is

Reign is a library to allow you to Rewrite Erlang In Go Nicely... that is,
by allowing you to do fairly direct ports of your existing Erlang code
without having to rearchitect it, or give up on clustering support.

The goal is NOT a precise translation; for one thing, that's just
plain impossible as both languages can easily do things the other can
not. In particular, Erlang's native concurrency is asynchronous
(that is, an Erlang message can be sent to a process whether or not
it is receiving), and Go can use user-defined types, both of which
have major impacts on properly structuring your program. The goal of
this library is to give you a scaffolding that you can use to rewrite
your Erlang code without having to entirely restructure it. In some sense,
ideally over time you should use less and less of this library, however
the goal is that this is production-quality and you should not be
forced to do so, especially as there aren't necessarily pure-Go
implementations of some of this functionality.

If you're using this to start a new Go project, you're probably doing it
wrong. Probably. Honestly the more I think about this statement the less
sure I am.

Reign itself is the clustering and mailbox code, which turns out to be
intimately related to each and require private access. For an
implementation of Supervisor Trees, see my "suture" library:
http://github.com/thejerf/suture .

To discuss how to use reign, first I will describe the mailbox/address system,
and how to use it to send and receive messages. Then we'll talk about how
to use this over a cluster.

Mailboxes and Addresses

Go channels are synchronous. Erlang-style messages are asynchronous. In
particular, this means that code written like this:

  receive
      {msg1, Msg} ->
           {echo, Msg, self()} ! AnotherPID,
           receive
               {echoResponse, Msg2} -> ...
           end
  end

while idiomatic Erlang, is not generally safe in Go with the standard
channels, because while you are processing a message, you are not able
to receive any others. Should you end up sending a message that is a
query to another such process, you'll deadlock on their finishing
processing their message. The best case is weird latencies, the worst
case is huge chunks of the system will end up deadlocked.

Of course, the correct Go answer is "don't do that", and when writing code
from scratch it is a solvable problem. However, it is very easy for
Erlang code to have accidentally embedded the asynchronous nature of
messages quite deeply into its architecture, especially given the number
of ways there are of using "receive" without realizing it. For instance,
any gen_server that makes a call to another gen_server is exhibiting this
pattern.

This implements a secondary message-passing system based on queues. You can
choose to "receive" specific messages as they come in by specifying a
match function on the messages, or simply receive the "next" message (a
useful optimization).

This is accomplished by creating two objects, a Mailbox and a matching
Address.

The Address object is the rough equivalent of a PID. It can be used
to send a message to the goroutine or goroutines receiving from the
Mailbox. It can be transmitted across the "clustering" communication
to transmit messages to goroutines in other OS processes.

The Mailbox is the recieving end. Processes serving the mailbox can
use this to receive messages. In Erlang this does not literally exist
because the "receive" keyword essentially takes "the mailbox of the current
process" as a hidden argument.

In particular, the Mailbox simulates the following bits of Erlang
message handling:

 * The asynchronous nature of sending a message to a PID. The mailbox
   receives independent of what the goroutine or goroutines handling
   it are doing.
 * The ability to reach into the Mailbox to choose what message to
   handle next, complete with performance implications.
 * The ability to send to Mailboxes that are not local to the current
   OS process
 * The ability to send Mailbox references that are network transparent
   within a given "cluster" (i.e., if you create a Mailbox locally and
   send a reference to it to another machine in the "cluster" somehow,
   remote machines will be able to reach it).

Resource Consumption

Mailboxes DO NOT create a goroutine. On their own, they're as dirt cheap as
any other moderately-sized ("several dozen bytes") structure.

The purpose of the following benchmarks is not to brag about the
implementation, as Erlang vs. Go is just so freaking different as to
be incomparable in many ways. For instance, the Erlang VM certainly
maintains a great deal more user-accessible metadata than either reign
or Go itself does. The purpose of the following benchmarks is merely to
establish that the library can maintain the same basic performance
guarantees as Erlang's VM; if your code can afford its communication
pattern as an Erlang program, translating it to Go should continue to be
at least that performant on the same hardware.

A Final Note

This is really classic open-source software. I really hope to have people
submitting pull requests, because the stated goal of the library is one of
those "infinite regression" sorts of things. Do you have some nice wrapper
around a "gen_server"-equivalent message passing paradigm? Is there some
other specifically-Erlang module that is missing that you've implemented
some support for? Please send pull requests.

*/
package reign

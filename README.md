reign 0.9.1
===========

[![Build Status](https://travis-ci.org/thejerf/reign.png?branch=master)](https://travis-ci.org/thejerf/reign)

Reign, "Rewrite Erlang In Go Nicely", is designed to make it easy to port
Erlang or Erlang-style programs into Go by providing a framework for
Erlang-style message passing, _including the creation of clusters_.

That is, this is not intended to be a "hey, look, if I wrap a mutex around
a slice I get something that looks like a PID" four hours of screwing
around in Go... this is intended to be a real, production-quality
replacement for Erlang-style message passing functionality, suitable for
porting existing Erlang programs out of Erlang without significant
architecture overhauls.

This module is fully covered with
[godoc](http://godoc.org/github.com/thejerf/reign), but test coverage is
not yet 100%. But it isn't bad.

If you do not need exactly Erlang-style semantics, note there are other
libraries out there that may be better starting points. But I don't know of
anything else for Go explicitly written with this goal in mind.

Current status: This is used in a production system in a 4-node cluster,
and has been through some trial-by-fire. But as is the nature of this code,
I'm sure there are more bugs we haven't tickled yet. I think if you want
to convert an existing Erlang code base to Go, this is likely to be very,
very helpful to you, but I don't promise it'll do everything you need
perfectly or that you won't need to work with me to make some things work.

Getting Started
===============

In order to address security concerns, reign uses TLS certificates signed
by a central CA, and all nodes mutually verify the CA's signature with each
other. To run reign at all requires creating a CA and certificates for each
node.

Since it's a bit of a pain to do this in openssl, especially if you don't
have it installed (Windows), a pure-go program to create some certificates
is provided, as well as a sample program. If you have where ever "go get"
puts binaries for you in your path, you can run:

   # go get github.com/thejerf/reign/cmd/reign_init
   # go get github.com/thejerf/reign/cmd/reign_sample
   # reign_init
   Signing certificate created
   Constructed certificate for node 1
   Constructed certificate for node 2

Note this will plop some certificates into your current directory.
You may want to start in a temporary directory or something.

Now, in two separate shells, you can run:

    # reign_sample 1
    # reign_sample 2

This starts up a simple chat server, as documented in the 
[sample.go](https://github.com/thejerf/reign/blob/master/cmd/reign_sample/sample.go) program. I
have chosen to create an example that focuses very specifically on the
message-passing; consequently it is terribly, terribly ugly on the
console. I assume you will prefer this to some slick presentation that
obscures the message-passing under the pile of code it takes to make a
halfway decent interface of any kind.

To get started, I suggest simply copying and pasting that example into a
new workspace and start working with the code. That way you start with a
working system the whole time.

Reign can use any certificates and certificate authorities that the Go TLS
library can handle. The certificates created by this script may not
entirely meet your needs. But they can get you started.

What Is "Erlang-like clustering"?
=================================

There are some common misconceptions about what "Erlang clustering"
is. Erlang gives you two things:

 * A PID which can be sent messages from anywhere within a cluster. The
   message itself may include PIDs, which remain live and useful through
   the transfer.
 * Mnesia, a shared dabatase, which can contain PIDs.

The misconception is that Erlang gives you some sort of magic automatic
clustering. It does not. It is still possible and even a bit easy to
build applications that are locked to a single OS process. It is your
job to take the Erlang primitives and build clusterable code.

What Is Reign?
==============

Reign is a library to allow you to *R*ewrite *E*rlang *I*n *G*o *N*icely... that is,
by allowing you to do fairly direct ports of your existing Erlang code
without having to rearchitect it, or give up on clustering support.

This package provides a PID-like data structure and the code to use
them across a cluster of Go processes, that may live on other machines.
It is expected that even code porting from Erlang will use other
database solutions, so there is no Mnesia equivalent, nor any desire
to implement such a thing.

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
wrong. Probably. Maybe. To be honest while I have not yet pulled the
trigger, I often find myself eyeing this library for my own projects even
so.

Mailboxes And Addresses
=======================

Addresses are the equvialent of Erlang PIDs. Mailboxes are the equivalent
of the implicit mailbox that an Erlang process has. Since goroutines can
not carry implicit data, they are explicit in Reign. To avoid confusion
about this unitary idea in Erlang being split in half in Reign, neither is
called a "PID", especially since it isn't a "process ID" anyhow.

To get the equivalent of a PID in Erlang, once you have the cluster
running, use NewMailbox from the connection service to receive a
paired Address and Mailbox. The Address may be moved across the cluster
transparently. The Mailbox is bound to the OS process that created it.
Mailboxes provide both the ability to simply receive the next message
and to pick a particular message out of the queue, with the same caveats
in Go as there are in Erlang about the dangers of the queue backing up
if you do not empty it.

In addition to asynchronous message queues and selective receive, 
reign implements an equivalent to "linking" in Erlang called
via OnCloseNotify. When used like Erlang linking, this
allows for some relatively safe RPC calling that will handle the
mailbox (or the relevant node) being entirely unattached while you
are trying to communicate with it. Exactly as with Erlang, this still
isn't a _guarantee_ of delivery, but it helps a lot.

Notes On Translation From Erlang
================================

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
channels if you try to naively replace the receive statements with
channel operations, because while you are processing a message, you are not able
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

If you do not know for sure that the only possible next message for a
mailbox is coming from the reply, you need to be sure to use .Receive()
with a proper selection function in such cases.

Known Issues
============

In Erlang, a PID secretly contains more information that identifies
something about when the node that originated the PID starts, or something
like that, which prevents the "same" PID from being used from two different
executions. This is easy to fix, but it isn't fixed yet.

I haven't yet benchmarked this very throughly, but generally speaking it
seems to perform comparably to Erlang at worst, and better in a lot
of common cases. There are obvious cases that are harder to compare,
such as the impact of the Go GC on your runtime. This code does
inevitably result in allocations.

Pre-1.0 Possible Changes
========================

  * The NodeID type may be unexported. I'm not sure it's ever directly
    useful to external code.
  * I'd like to change the logging code to log with JSON objects that
    happen to render to strings, allowing transparent use with logrus.

Contribution Opportunities
==========================

Of course, it's open source. Contribute whatever you like. But in addition
to resolving the known issues or the pre-1.0 possible changes, I would
certainly be interested in:

  * More test coverage.
  * Adding an element to the Address that includes the node's start-up
    time or some other element that will be different between each run,
    so an address can't be obtained from one OS process, then that process
    dies and another starts in its place, and then someone re-uses that
    ID. Erlang has this.
  * Dynamically modifying the cluster at run time. Conceptually it's not
    too hard, "just work", but there's some i's to dot and t's to cross.
    I have some questions around the UI for this.
  * Is it possible to efficiently avoid the need to register message types?
    I'm vaguely nervous about looking at the gob error message and trying
    to infer whether I should call "register", or similar things, but it
    would make this easier to use.
  * Pluggable serialization types. Again, conceptually not that difficult,
    "just work".
  * Any additional similar Erlang-like services _that are not covered by
    other packages_, that are useful for Erlang code porting. e.g., I'm not
    really that interested in an Mnesia-like service for reign, because
    there are so many good database choices nowadays. (If you want, you can
    create such a thing, but make it a separate project. I'll happily link
    it.)
  * A coworker was doing some experimentation on having the mailboxes use
    channels instead of sync.Cond. It is not clear to either of whether
    this really clarified the code, or whether it would offer any
    performance advantages. (Unfortunately, it still wouldn't allow you to
    put a mailbox in a select statement. Mailbox semantics are
    fundamentally incompatible with select, as they are fundamentally
    asynchronous.) It certainly added a lot of lines of code.
  * Additional debugging capabilities. It would be interesting to split the
    private mailbox stuff off into a "reign/mailbox" package that made a
    lot more of the internals public for debugging, and then have the
    current mailbox.go offer the same external interface, but nicely
    "sealed". Using reign/mailbox would be something you might not want to
    do in production. But I've found it very useful over the years in
    Erlang to be able to peek into the mailboxes for diagnostics.

    And there's other capabilities that may need to be exposed, or need to
    better documented, or something. Another interesting idea would be to
    add an optional interface like `type TracedMsg { ReignSent(); ReignReceived() }`
    that can be called for an implementing type whenever a message is
    sent, received, transferred across a cluster, etc.

    Erlang's got certain advantages that barring the adoption of a
    standardized dynamic embedded language for Go, we can't really
    offer. But we can offer what we can!
  * On that note, any sort of integration with any REPL for Go would be
    accepted. I would hope it would take the form of implementing certain
    interfaces or something and not deep integration into the data types,
    though.

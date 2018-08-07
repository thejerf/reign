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

If this library sounds interesting to you, I would invite you to work with
me on improving this code rather than starting again on your own. If you
need help or more documentation, ask and ye shall receive.

If you do not need exactly Erlang-style semantics, note there are other
libraries out there that may be better starting points. But I don't know of
anything else for Go explicitly written with this goal in mind.

Getting Started
===============

In order to address security concerns, reign uses TLS certificates signed
by a central CA, and all nodes mutually verify the CA's signature with each
other. To run reign at all requires creating a CA and certificates for each
node.

Since it's a bit of a pain to do this in openssl, especially if you don't
have it installed (Windows), a pure-go program to create some certificates
is provided:

   # go get github.com/thejerf/reign/cmd/reign_init
   # reign_init
   Signing certificate created
   Constructed certificate for node 1
   Constructed certificate for node 2

This command created six files, `reign_ca.cert`, `reign_ca.key`,
`reign_node.1.crt`, `reign_node.1.key`, `reign_node.2.crt`, and
`reign_node.2.key`.

Reign can use any certificates and certificate authorities that the Go TLS
library can handle. The certificates created by this script may not
entirely meet your needs. But they can get you started.

You can now use these certificates to run the sample code.

Contribution Opportunities
==========================

Of course, it's open source. Contribute whatever you like. But I would
certainly be interested in:

  * Dynamically modifying the node list at run time. Conceptually it's not
    too hard, "just work", but there's some i's to dot and t's to cross.
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

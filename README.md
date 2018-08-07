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

reign
=====

[![Build Status](https://travis-ci.org/thejerf/reign.png?branch=master)](https://travis-ci.org/thejerf/reign)

Reign, "Rewrite Erlang In Go Nicely", is designed to make it easy to port Erlang
or Erlang-style programs into Go by providing a framework for Erlang-style
message passing, _including the creation of clusters_.

That is, this is not intended to be a "hey, look, if I wrap a mutex around
a slice I get something that looks like a PID" four hours of screwing
around in Go... this is intended to be a real, production-quality
replacement for Erlang-style message passing functionality, suitable for
porting existing Erlang programs out of Erlang without significant
architecture overhauls.

That said, it is not there yet. The local message passing functionality is,
I believe, correct and API-stable, but the clustering is still in progress.

PLEASE DO NOT SUBMIT TO REDDIT, HACKER NEWS, ETC... while I'd like to put
this on there eventually, first I'd like the examples, coverage, working
clustering, etc. This library is being put up on GitHub even in this state
because there was some interest in seeing it as-is. I am interested in
pull-requests, and I am even interested just in emails that say that you
are interested, as I work out how to spend my copious spare time.

Blog posts will probably appear on this topic on my blog, but there are
none yet.

This module is fully covered with
[godoc](http://godoc.org/github.com/thejerf/reign), but test coverage is
not yet 100%, and there's no example yet (which I intend to get to Real
Soon Now (TM)). But there is a substantial test suite, which will be kept
passing, coverage is currently at 84.4% of the code (and if you feel like
contributing, a good place to start is by simply trying to increase that
number), and I will be responsive to requests.

If this library sounds interesting to you, I would invite you to work with
me on improving this code rather than starting again on your own. If you
need help or more documentation, ask and ye shall receive.

If you do not need exactly Erlang-style semantics, note there are other
libraries out there that may be better starting points. But I don't know of
anything else for Go explicitly written with this goal in mind.

Security
========

One note about reign is that I wanted to take the opportunity to fix the
security of Erlang clustering by making all clustering run across SSL TCP
connections. See the README.certificate.md for information about how to
create the requisite certificates.

Preliminary benchmarking seems to indicate that the overhead of running
everything through SSL is negligible, so an earlier feature allowing you to
choose whether you use it has been eliminated since it added significant
complexity and danger with no benefits.

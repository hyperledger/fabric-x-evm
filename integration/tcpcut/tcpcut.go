/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package tcpcut provides a transparent TCP proxy whose connections a test can
// sever on demand, for exercising how a component recovers when a connection it
// depends on drops.
//
// Point the component's configured endpoint at the proxy instead of the real
// service, then call Cut:
//
//	proxy := tcpcut.New(t, cfg.Committer.Endpoint.Address())
//	// ... configure the component with proxy.Port() ...
//	proxy.Cut() // the component's stream fails; it must recover on its own
//
// Cut models a blip: the component reconnects as soon as it tries, which is often
// immediately.  Use Hold and Release instead when the test needs the outage to last
// long enough for something to happen behind it:
//
//	proxy.Hold()    // connection down, and reconnects refused
//	// ... let the world move on without the component ...
//	proxy.Release() // reconnects accepted again
//
// The proxy forwards raw bytes and never inspects them, so TLS — including mTLS —
// is negotiated end-to-end between the real client and the real server and passes
// through untouched; the proxy needs none of the certificates.  That is the point
// of cutting at the TCP layer rather than reaching into a gRPC connection's
// internals to close its socket: nothing here depends on the transport's
// implementation details, so it does not break when gRPC or an SDK is upgraded.
package tcpcut

import (
	"io"
	"net"
	"sync"
	"testing"
)

// Proxy is a transparent TCP proxy in front of a single upstream address.
// Create one with New.
//
// Cut closes both sides of every live connection, which the client observes as a
// transport error on its stream.  Connections opened afterwards are accepted
// normally, so a client that reconnects succeeds — a Cut models a transient
// outage, not a dead service.
//
// Hold is Cut plus a refusal to carry new connections until Release, for tests that
// need the outage to last rather than to happen.
type Proxy struct {
	listener net.Listener
	upstream string

	mu    sync.Mutex
	conns []net.Conn // both sides of every live connection, for Cut to close
	// held is set by Hold and cleared by Release.  While set, accepted connections
	// are closed instead of being forwarded, so a client's reconnect attempts keep
	// failing for as long as the test needs them to.
	held   bool
	closed bool

	wg sync.WaitGroup
}

// New starts a proxy in front of upstream ("host:port") on a port the OS picks,
// and registers its shutdown on t.  Use Addr or Port to configure the client
// under test.  Fails t if the listener cannot be opened.
func New(t *testing.T, upstream string) *Proxy {
	t.Helper()

	// Listen on 127.0.0.1 specifically: callers' configs use IPv4 literals, and
	// binding the wildcard address would also expose the proxy off-box.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcpcut: listen for proxy to %s: %v", upstream, err)
	}

	p := &Proxy{listener: l, upstream: upstream}
	p.wg.Add(1)
	go p.serve()
	t.Cleanup(p.Close)
	return p
}

// Addr is the proxy's own address, in "host:port" form.
func (p *Proxy) Addr() net.Addr { return p.listener.Addr() }

// Port is the proxy's own port, for configs that take host and port separately.
func (p *Proxy) Port() int { return p.listener.Addr().(*net.TCPAddr).Port }

func (p *Proxy) serve() {
	defer p.wg.Done()
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return // listener closed by Close
		}

		// Held: refuse the connection rather than forwarding it.  Checked before
		// dialling so the upstream never sees a connection the test means to withhold,
		// and the client sees its reconnect fail.
		if p.isHeld() {
			_ = client.Close()
			continue
		}

		server, err := net.Dial("tcp", p.upstream)
		if err != nil {
			// The upstream is down; drop the client and let it retry.
			_ = client.Close()
			continue
		}

		switch p.track(client, server) {
		case trackRefused:
			continue // held while we were dialling
		case trackClosed:
			return // closed while we were dialling
		}

		p.wg.Add(2)
		go p.pipe(client, server)
		go p.pipe(server, client)
	}
}

// trackResult reports what track did with the connection it was given.
type trackResult int

const (
	trackOK      trackResult = iota // tracked; forward it
	trackRefused                    // held: dropped, but keep serving
	trackClosed                     // proxy closed: stop serving
)

// track registers both sides of a connection so Cut and Close can reach them.
//
// The held and closed checks happen here, under the same lock that Hold and Close
// take, because either can land while serve is dialling upstream: without this a
// connection opened just before Hold would be forwarded for the whole outage, and
// the test would silently get the race it was trying to remove.
func (p *Proxy) track(client, server net.Conn) trackResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch {
	case p.closed:
		_ = client.Close()
		_ = server.Close()
		return trackClosed
	case p.held:
		_ = client.Close()
		_ = server.Close()
		return trackRefused
	}
	p.conns = append(p.conns, client, server)
	return trackOK
}

// isHeld reports whether the proxy is currently refusing connections.
func (p *Proxy) isHeld() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.held
}

// pipe copies from src to dst until either end closes.  Both directions of a
// connection run one of these; whichever finishes first closes the write side so
// the other unblocks.
func (p *Proxy) pipe(src, dst net.Conn) {
	defer p.wg.Done()
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}

// Cut closes every live connection through the proxy.  The client sees its stream
// fail; a reconnect afterwards is accepted normally.
func (p *Proxy) Cut() {
	p.drop(false)
}

// Hold cuts every live connection and refuses new ones until Release, so the
// outage lasts as long as the test needs rather than until the client's next
// reconnect attempt.
//
// Setting the flag and cutting under one lock is what makes the outage airtight:
// done as two steps, a reconnect landing between them would be forwarded and the
// client would keep seeing traffic through an outage that was supposed to be total.
//
// The listener stays bound while held, so a client configured with this port keeps
// getting a refused connection rather than a dial failure, and the port cannot be
// taken by something else before Release.
func (p *Proxy) Hold() {
	p.drop(true)
}

// Release resumes carrying connections after Hold.  Connections cut by the Hold are
// gone for good; the client is expected to reconnect, which now succeeds.
// Safe to call without a preceding Hold.
func (p *Proxy) Release() {
	p.mu.Lock()
	p.held = false
	p.mu.Unlock()
}

// drop closes every live connection, optionally marking the proxy held first.
// Both happen under one lock so no connection can slip into the gap; the closes
// themselves happen outside it, since Close on a piped connection unblocks a pipe
// goroutine and there is no reason to hold the lock through that.
func (p *Proxy) drop(hold bool) {
	p.mu.Lock()
	if hold {
		p.held = true
	}
	conns := p.conns
	p.conns = nil
	p.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}
}

// Close stops accepting, drops every live connection, and waits for the proxy's
// goroutines to exit.  Registered on t by New; safe to call twice.
func (p *Proxy) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()

	_ = p.listener.Close()
	p.Cut()
	p.wg.Wait()
}

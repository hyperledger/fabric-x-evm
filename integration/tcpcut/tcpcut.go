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
type Proxy struct {
	listener net.Listener
	upstream string

	mu     sync.Mutex
	conns  []net.Conn // both sides of every live connection, for Cut to close
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

		server, err := net.Dial("tcp", p.upstream)
		if err != nil {
			// The upstream is down; drop the client and let it retry.
			_ = client.Close()
			continue
		}

		if !p.track(client, server) {
			return // closed while we were dialling
		}

		p.wg.Add(2)
		go p.pipe(client, server)
		go p.pipe(server, client)
	}
}

// track registers both sides of a connection so Cut and Close can reach them.
// Returns false if the proxy has already been closed.
func (p *Proxy) track(client, server net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = client.Close()
		_ = server.Close()
		return false
	}
	p.conns = append(p.conns, client, server)
	return true
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
	p.mu.Lock()
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

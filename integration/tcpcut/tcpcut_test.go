/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package tcpcut

import (
	"bufio"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// echoServer is a trivial TCP server that echoes each line back, used to prove
// the proxy forwards bytes in both directions and that Cut is observable to a
// client as a broken connection.
func echoServer(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return l
}

// TestProxy_ForwardsAndCuts covers the behaviour callers depend on: traffic
// flows through the proxy, Cut breaks a live connection, and a fresh connection
// afterwards works again (a transient outage, not a dead service).
func TestProxy_ForwardsAndCuts(t *testing.T) {
	upstream := echoServer(t)
	proxy := New(t, upstream.Addr().String())

	dial := func() net.Conn {
		t.Helper()
		c, err := net.DialTimeout("tcp", proxy.Addr().String(), 5*time.Second)
		if err != nil {
			t.Fatalf("dial proxy: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}

	roundTrip := func(c net.Conn, msg string) (string, error) {
		if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return "", err
		}
		if _, err := io.WriteString(c, msg+"\n"); err != nil {
			return "", err
		}
		return bufio.NewReader(c).ReadString('\n')
	}

	// Forwarding works in both directions.
	conn := dial()
	got, err := roundTrip(conn, "hello")
	if err != nil {
		t.Fatalf("round trip through proxy: %v", err)
	}
	if got != "hello\n" {
		t.Fatalf("proxy returned %q, want %q", got, "hello\n")
	}

	// Cut breaks the live connection: the client's next use of it fails.
	proxy.Cut()
	if _, err := roundTrip(conn, "after-cut"); err == nil {
		t.Error("connection still usable after Cut; the client would never see a stream error")
	} else if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		// Any failure is acceptable — this just records what it looked like.
		t.Logf("post-cut error (expected some failure): %v", err)
	}

	// A reconnect succeeds, which is what lets the synchronizer recover.
	got, err = roundTrip(dial(), "reconnected")
	if err != nil {
		t.Fatalf("round trip after reconnect: %v", err)
	}
	if got != "reconnected\n" {
		t.Fatalf("proxy returned %q after reconnect, want %q", got, "reconnected\n")
	}
}

// TestProxy_HoldRefusesReconnects covers the guarantee Hold exists for, and that
// the callers' tests silently depend on: while held, a client cannot get back
// through no matter how promptly it retries.  A Hold that failed to block
// reconnects would leave those tests passing while exercising nothing, so the
// property is checked here rather than assumed.
func TestProxy_HoldRefusesReconnects(t *testing.T) {
	upstream := echoServer(t)
	proxy := New(t, upstream.Addr().String())

	roundTrip := func(c net.Conn, msg string) (string, error) {
		if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return "", err
		}
		if _, err := io.WriteString(c, msg+"\n"); err != nil {
			return "", err
		}
		return bufio.NewReader(c).ReadString('\n')
	}

	// A connection taken out before the Hold, to confirm Hold cuts live traffic too
	// and does not merely refuse new connections.
	live, err := net.DialTimeout("tcp", proxy.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer live.Close()
	if _, err := roundTrip(live, "before-hold"); err != nil {
		t.Fatalf("round trip before Hold: %v", err)
	}

	proxy.Hold()

	if _, err := roundTrip(live, "during-hold"); err == nil {
		t.Error("the pre-existing connection still works while held; Hold must cut live traffic as well")
	}

	// Retry hard: the dial itself succeeds, because the listener stays bound, but the
	// proxy must drop the connection instead of forwarding it.  Every attempt has to
	// fail — one that got through would be exactly the race Hold removes.
	for i := range 20 {
		c, err := net.DialTimeout("tcp", proxy.Addr().String(), 5*time.Second)
		if err != nil {
			continue // a refused dial is a failed reconnect too
		}
		got, err := roundTrip(c, "reconnect-attempt")
		_ = c.Close()
		if err == nil {
			t.Fatalf("attempt %d reconnected through a held proxy and got %q; "+
				"an outage that lets traffic through is no outage", i, got)
		}
	}

	// Release restores service, which is what lets the component under test recover.
	proxy.Release()
	c, err := net.DialTimeout("tcp", proxy.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial after Release: %v", err)
	}
	defer c.Close()
	got, err := roundTrip(c, "released")
	if err != nil {
		t.Fatalf("round trip after Release: %v", err)
	}
	if got != "released\n" {
		t.Fatalf("proxy returned %q after Release, want %q", got, "released\n")
	}
}

// TestProxy_CloseIsIdempotent guards the t.Cleanup path: New registers Close,
// and a test may also call it explicitly.
func TestProxy_CloseIsIdempotent(t *testing.T) {
	upstream := echoServer(t)
	proxy := New(t, upstream.Addr().String())

	c, err := net.DialTimeout("tcp", proxy.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer c.Close()

	proxy.Close()
	proxy.Close() // must not panic or block
}

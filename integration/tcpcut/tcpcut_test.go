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

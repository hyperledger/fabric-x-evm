/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"

	"github.com/hyperledger/fabric-x-evm/gateway/api/filters"
)

func TestNewServer_RegistersFilterAPI(t *testing.T) {
	feed := filters.NewBlockFeed()
	defer feed.Close()

	rpcSrv, err := NewServer(&stubBackend{chainID: big.NewInt(4011), blockNum: 1}, feed)
	if err != nil {
		t.Fatal(err)
	}
	client := rpc.DialInProc(rpcSrv)

	var id string
	if err := client.Call(&id, "eth_newBlockFilter"); err != nil {
		t.Fatalf("eth_newBlockFilter: %v", err)
	}
	if id == "" {
		t.Fatal("empty filter id")
	}
	var ok bool
	if err := client.Call(&ok, "eth_uninstallFilter", id); err != nil || !ok {
		t.Fatalf("uninstall: ok=%v err=%v", ok, err)
	}
}

func newTestHTTPServer(t *testing.T) string {
	t.Helper()
	rpcSrv, err := NewServer(&stubBackend{chainID: big.NewInt(4011)}, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	httpSrv := NewHTTPServer(rpcSrv, ln.Addr().String())
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	})
	return ln.Addr().String()
}

func TestWebSocket_ChainID(t *testing.T) {
	addr := newTestHTTPServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := rpc.DialContext(ctx, "ws://"+addr)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer client.Close()

	var chainID hexutil.Big
	if err := client.CallContext(ctx, &chainID, "eth_chainId"); err != nil {
		t.Fatalf("eth_chainId over ws: %v", err)
	}
	if (*big.Int)(&chainID).Cmp(big.NewInt(4011)) != 0 {
		t.Fatalf("chainId = %s, want 4011", (*big.Int)(&chainID).String())
	}
}

func TestHTTPAndWebSocket_Concurrent(t *testing.T) {
	addr := newTestHTTPServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	httpClient, err := rpc.DialContext(ctx, "http://"+addr)
	if err != nil {
		t.Fatalf("http dial: %v", err)
	}
	defer httpClient.Close()

	wsClient, err := rpc.DialContext(ctx, "ws://"+addr)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer wsClient.Close()

	var httpID, wsID hexutil.Big
	if err := httpClient.CallContext(ctx, &httpID, "eth_chainId"); err != nil {
		t.Fatalf("http eth_chainId: %v", err)
	}
	if err := wsClient.CallContext(ctx, &wsID, "eth_chainId"); err != nil {
		t.Fatalf("ws eth_chainId: %v", err)
	}
	want := big.NewInt(4011)
	if (*big.Int)(&httpID).Cmp(want) != 0 || (*big.Int)(&wsID).Cmp(want) != 0 {
		t.Fatalf("http=%s ws=%s, want both 4011", (*big.Int)(&httpID), (*big.Int)(&wsID))
	}
}

func TestHTTP_WithoutUpgradeStillWorks(t *testing.T) {
	addr := newTestHTTPServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := rpc.DialContext(ctx, "http://"+addr)
	if err != nil {
		t.Fatalf("http dial: %v", err)
	}
	defer client.Close()

	var listening bool
	if err := client.CallContext(ctx, &listening, "net_listening"); err != nil {
		t.Fatalf("net_listening: %v", err)
	}
	if !listening {
		t.Fatal("net_listening = false")
	}
}

func TestWebSocket_OriginChecks(t *testing.T) {
	addr := newTestHTTPServer(t)
	url := "ws://" + addr

	t.Run("evil origin rejected", func(t *testing.T) {
		hdr := http.Header{}
		hdr.Set("Origin", "https://evil.example")
		_, resp, err := websocket.DefaultDialer.Dial(url, hdr)
		if err == nil {
			t.Fatal("expected dial failure for evil Origin")
		}
		if resp == nil {
			t.Fatalf("expected HTTP response on rejected handshake, err=%v", err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
		}
	})

	t.Run("no origin accepted", func(t *testing.T) {
		conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("dial without Origin: %v (status=%d)", err, status)
		}
		_ = conn.Close()
	})

	t.Run("localhost origin accepted", func(t *testing.T) {
		hdr := http.Header{}
		hdr.Set("Origin", "http://localhost")
		conn, resp, err := websocket.DefaultDialer.Dial(url, hdr)
		if err != nil {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("dial with localhost Origin: %v (status=%d)", err, status)
		}
		_ = conn.Close()
	})
}

func TestIsWebsocket(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if isWebsocket(req) {
		t.Fatal("plain GET should not be websocket")
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	if !isWebsocket(req) {
		t.Fatal("expected websocket upgrade")
	}
}

/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-lib-go/common/flogging"

	"github.com/hyperledger/fabric-x-evm/gateway/api/filters"
)

var apiLogger = flogging.MustGetLogger("gateway.api")

// NewServer returns an RPC server.
// feed may be nil to skip eth_*Filter methods (unit tests that do not exercise filters).
func NewServer(b Backend, feed *filters.BlockFeed) (*rpc.Server, error) {
	srv := rpc.NewServer()
	if err := srv.RegisterName("eth", NewEthAPI(b)); err != nil {
		return nil, err
	}
	if feed != nil {
		if err := srv.RegisterName("eth", filters.NewFilterAPI(feed, b)); err != nil {
			return nil, err
		}
	}

	chainID, err := b.ChainID(context.TODO())
	if err != nil {
		return nil, err
	}
	if err := srv.RegisterName("net", NewNetAPI(chainID.String())); err != nil {
		return nil, err
	}
	if err := srv.RegisterName("web3", NewWeb3API()); err != nil {
		return nil, err
	}

	return srv, nil
}

// NewHTTPServer creates and configures an HTTP server without starting it.
// The same listener serves JSON-RPC over HTTP and WebSocket: Upgrade requests
// go to srv.WebsocketHandler (nil origins = geth default handshake checks);
// everything else uses the HTTP stack. Logging wraps the HTTP path only so WS
// upgrades can Hijack the connection.
func NewHTTPServer(srv *rpc.Server, addr string) *http.Server {
	// nil cors disables the CORS middleware; nil vhosts still allows IP Hosts
	// (127.0.0.1 etc.) via geth's virtualHostHandler.
	httpHandler := node.NewHTTPHandlerStack(srv, nil, nil, nil)
	return &http.Server{
		Addr: addr,
		Handler: &rpcTransportHandler{
			ws:   srv.WebsocketHandler(nil),
			http: &loggingHandler{next: httpHandler},
		},
	}
}

// rpcTransportHandler dispatches WebSocket upgrades before HTTP, matching
// go-ethereum's httpServer.ServeHTTP check order (node/rpcstack.go).
type rpcTransportHandler struct {
	ws   http.Handler
	http http.Handler
}

func (h *rpcTransportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWebsocket(r) {
		h.ws.ServeHTTP(w, r)
		return
	}
	h.http.ServeHTTP(w, r)
}

// isWebsocket mirrors go-ethereum's unexported helper in node/rpcstack.go.
func isWebsocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

type loggingHandler struct {
	next http.Handler
}

func (h *loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close() //nolint:errcheck

	apiLogger.Debugf("[req] %s", body)

	r.Body = io.NopCloser(bytes.NewReader(body))

	// TODO: this hack disables gzip, which is useful for debugging
	r.Header.Del("Accept-Encoding")

	rec := &responseRecorder{ResponseWriter: w}

	h.next.ServeHTTP(rec, r)
}

type responseRecorder struct {
	http.ResponseWriter
	body []byte
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return r.ResponseWriter.Write(b)
}

type NetAPI struct {
	networkID string
}

func NewNetAPI(networkID string) *NetAPI {
	return &NetAPI{networkID: networkID}
}

// net_version
func (api *NetAPI) Version() string {
	return api.networkID
}

// net_listening
func (api *NetAPI) Listening() bool {
	return true
}

type Web3API struct{}

func NewWeb3API() *Web3API {
	return &Web3API{}
}

// web3_clientVersion
func (api *Web3API) ClientVersion() string {
	return "fabric-evm/0.1.0"
}

/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"

	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
)

// NewServer returns an RPC server.
func NewServer(b Backend) (*rpc.Server, error) {
	srv := rpc.NewServer()
	if err := srv.RegisterName("eth", NewEthAPI(b)); err != nil {
		return nil, err
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
func NewHTTPServer(srv *rpc.Server, addr string) *http.Server {
	handler := node.NewHTTPHandlerStack(srv, []string{"*"}, []string{"*"}, nil)
	return &http.Server{
		Addr:    addr,
		Handler: &loggingHandler{next: handler},
	}
}

type loggingHandler struct {
	next http.Handler
}

func (h *loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close() //nolint:errcheck

	log.Printf("[req] %s", body)

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
	// Return HardhatNetwork for compatibility with Hardhat's network detection
	return "HardhatNetwork/fabric-evm/0.1.0"
}

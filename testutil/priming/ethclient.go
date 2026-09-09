/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package priming

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
)

// NewNativeEthClient dials gw in-process over the production RPC surface, so
// callers can read committed state back through the same API a real client uses.
func NewNativeEthClient(gw *core.Gateway) (*ethclient.Client, error) {
	// Create production RPC server (no test accounts needed for integration tests)
	rpcServer, err := gwapi.NewServer(gw)
	if err != nil {
		return nil, err
	}

	client := rpc.DialInProc(rpcServer)
	return ethclient.NewClient(client), nil
}

// WaitForCommit blocks until tx is no longer pending, or ctx (bounded to 10s) expires.
func WaitForCommit(ctx context.Context, ec *ethclient.Client, tx *types.Transaction) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var err error

	backoff := time.Duration(0)
	iter := 0
	step := 100

	for pending := true; pending; {
		_, pending, err = ec.TransactionByHash(ctx, tx.Hash())
		if err != nil {
			if !strings.Contains(err.Error(), "not found") {
				return fmt.Errorf("waiting for tx %s to commit: %w", tx.Hash(), err)
			}
			pending = true
		}

		if pending {
			if backoff == 0 {
				runtime.Gosched()
			} else {
				time.Sleep(backoff)
			}

			iter++
			if iter%step == 0 {
				if backoff == 0 {
					backoff = time.Millisecond
				} else {
					backoff *= 2
				}
			}
		}
	}

	return nil
}

/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// DirectiveEndorser is a test-only wrapper adding non-EVM system directive handling
// (e.g. hardhat_setBalance); everything else is delegated to the wrapped endorser,
// which refuses directives itself.
type DirectiveEndorser struct {
	api.Service

	kvs               execution.KVSSnapshotter
	namespace         string
	builder           endorsement.Builder
	monotonicVersions bool
}

var _ api.Service = (*DirectiveEndorser)(nil)

// NewDirectiveEndorser wraps inner with directive handling; kvs, namespace, builder
// and monotonicVersions must match what the wrapped endorser was constructed with.
func NewDirectiveEndorser(
	inner api.Service,
	kvs execution.KVSSnapshotter,
	namespace string,
	builder endorsement.Builder,
	monotonicVersions bool,
) *DirectiveEndorser {
	return &DirectiveEndorser{
		Service:           inner,
		kvs:               kvs,
		namespace:         namespace,
		builder:           builder,
		monotonicVersions: monotonicVersions,
	}
}

// SetBalance forces addr's balance to amount and endorses the resulting read-write
// set. There is no StateDB.SetBalance, so the target is reached by delta.
func (d *DirectiveEndorser) SetBalance(ctx context.Context, inv endorsement.Invocation, addr common.Address, amount *big.Int) (*peer.ProposalResponse, error) {
	target, overflow := uint256.FromBig(amount)
	if overflow {
		return nil, fmt.Errorf("setBalance directive: amount overflows uint256")
	}

	reader, err := d.kvs.NewSnapshot(nil)
	if err != nil {
		return nil, fmt.Errorf("setBalance directive: snapshot: %w", err)
	}
	defer reader.Close()

	stateDB, err := execution.NewStateDB(ctx, reader, d.namespace, 0, d.monotonicVersions)
	if err != nil {
		return nil, fmt.Errorf("setBalance directive: statedb: %w", err)
	}

	current := stateDB.GetBalance(addr)
	switch target.Cmp(current) {
	case 1: // raise to target
		stateDB.AddBalance(addr, new(uint256.Int).Sub(target, current), tracing.BalanceChangeUnspecified)
	case -1: // lower to target
		stateDB.SubBalance(addr, new(uint256.Int).Sub(current, target), tracing.BalanceChangeUnspecified)
	}
	return d.builder.Endorse(inv, endorsement.Success(stateDB.Result(), nil, nil))
}

// SetCode forces addr's code to code and endorses the resulting read-write set.
// It does not call CreateAccount, so it never touches balance or nonce.
func (d *DirectiveEndorser) SetCode(ctx context.Context, inv endorsement.Invocation, addr common.Address, code []byte) (*peer.ProposalResponse, error) {
	reader, err := d.kvs.NewSnapshot(nil)
	if err != nil {
		return nil, fmt.Errorf("setCode directive: snapshot: %w", err)
	}
	defer reader.Close()

	stateDB, err := execution.NewStateDB(ctx, reader, d.namespace, 0, d.monotonicVersions)
	if err != nil {
		return nil, fmt.Errorf("setCode directive: statedb: %w", err)
	}

	stateDB.SetCode(addr, code, tracing.CodeChangeUnspecified)
	return d.builder.Endorse(inv, endorsement.Success(stateDB.Result(), nil, nil))
}

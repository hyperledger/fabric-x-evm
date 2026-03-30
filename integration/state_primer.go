/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	pb "github.com/hyperledger/fabric-protos-go-apiv2/peer"

	"github.com/hyperledger/fabric-x-evm/endorser"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"github.com/hyperledger/fabric-x-sdk/network"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// StatePrimer provides a builder pattern for priming ledger state.
// It allows setting nonces, code, balances, and storage for addresses,
// then commits all changes in a single transaction.
type StatePrimer struct {
	db        *state.VersionedDB
	namespace string
	signer    sdk.Signer
	builders  []endorsement.Builder
	submitter interface {
		Submit(ctx context.Context, env sdk.Endorsement) error
	}
	channel           string
	nsVersion         string
	monotonicVersions bool

	// Simulation store that caches all state changes
	sim     *state.SimulationStore
	stateDB vm.StateDB
}

// NewStatePrimer creates a new state primer builder.
func NewStatePrimer(
	db *state.VersionedDB,
	namespace string,
	signer sdk.Signer,
	builders []endorsement.Builder,
	submitter interface {
		Submit(ctx context.Context, env sdk.Endorsement) error
	},
	channel string,
	nsVersion string,
	monotonicVersions bool,
) (*StatePrimer, error) {
	// Create a simulation store immediately
	sim, err := state.NewSimulationStore(context.TODO(), db, namespace, 0, monotonicVersions)
	if err != nil {
		return nil, err
	}

	// Create a SnapshotDB to apply operations
	stateDB := endorser.NewSnapshotDB(sim)

	return &StatePrimer{
		db:                db,
		namespace:         namespace,
		signer:            signer,
		builders:          builders,
		submitter:         submitter,
		channel:           channel,
		nsVersion:         nsVersion,
		monotonicVersions: monotonicVersions,
		sim:               sim,
		stateDB:           stateDB,
	}, nil
}

// SetNonce sets the nonce for an address immediately in the simulation store.
func (sp *StatePrimer) CreateAccount(addr common.Address) *StatePrimer {
	sp.stateDB.CreateAccount(addr)
	return sp
}

// SetNonce sets the nonce for an address immediately in the simulation store.
func (sp *StatePrimer) SetNonce(addr common.Address, nonce uint64) *StatePrimer {
	sp.stateDB.SetNonce(addr, nonce, tracing.NonceChangeEoACall)
	return sp
}

// SetCode sets the code for an address immediately in the simulation store.
func (sp *StatePrimer) SetCode(addr common.Address, code []byte) *StatePrimer {
	sp.stateDB.SetCode(addr, code, tracing.CodeChangeUnspecified)

	return sp
}

// SetBalance sets the balance for an address immediately in the simulation store.
func (sp *StatePrimer) SetBalance(addr common.Address, balance *big.Int) *StatePrimer {
	if balance != nil && balance.Sign() > 0 {
		// Use AddBalance to set the balance (SnapshotDB doesn't have SetBalance)
		// This works because accounts start with zero balance
		sp.stateDB.AddBalance(addr, uint256.MustFromBig(balance), tracing.BalanceChangeUnspecified)
	}
	return sp
}

// SetStorage sets storage slots for an address immediately in the simulation store.
func (sp *StatePrimer) SetStorage(addr common.Address, storage map[common.Hash]common.Hash) *StatePrimer {
	for key, value := range storage {
		sp.stateDB.SetState(addr, key, value)
	}
	return sp
}

// SetAccount queues setting multiple properties for an address at once.
func (sp *StatePrimer) SetAccount(addr common.Address, nonce *uint64, code []byte, balance *big.Int, storage map[common.Hash]common.Hash) *StatePrimer {
	sp.CreateAccount(addr)

	if nonce != nil {
		sp.SetNonce(addr, *nonce)
	}

	sp.SetCode(addr, code)

	if balance != nil {
		sp.SetBalance(addr, balance)
	}
	if len(storage) > 0 {
		sp.SetStorage(addr, storage)
	}
	return sp
}

// LoadFromJSON loads state priming operations from a JSON file.
// The JSON format matches the AllocEntry structure used in genesis files.
func (sp *StatePrimer) LoadFromJSON(jsonFilePath string) (*StatePrimer, error) {
	if jsonFilePath == "" {
		return sp, nil
	}

	data, err := os.ReadFile(jsonFilePath)
	if err != nil {
		return nil, err
	}

	var alloc map[string]AllocEntry
	if err := json.Unmarshal(data, &alloc); err != nil {
		return nil, err
	}

	for addrStr, entry := range alloc {
		addr := common.HexToAddress(addrStr)

		// Parse nonce
		var nonce *uint64
		if entry.Nonce != "" {
			n, _ := new(big.Int).SetString(entry.Nonce[2:], 16) // skip "0x"
			nonceVal := n.Uint64()
			nonce = &nonceVal
		}

		// Parse balance
		var balance *big.Int
		if entry.Balance != "" {
			balance, _ = new(big.Int).SetString(entry.Balance[2:], 16)
		}

		// Parse code
		var code []byte
		if entry.Code != "" && entry.Code != "0x" {
			code = common.FromHex(entry.Code)
		}

		// Parse storage
		var storage map[common.Hash]common.Hash
		if len(entry.Storage) > 0 {
			storage = make(map[common.Hash]common.Hash)
			for k, v := range entry.Storage {
				key := common.HexToHash(k)
				value := common.HexToHash(v)
				storage[key] = value
			}
		}

		sp.SetAccount(addr, nonce, code, balance, storage)
	}

	return sp, nil
}

// Commit applies all state changes to the ledger by creating a proposal,
// endorsing it, and submitting it through the normal Fabric commit flow.
func (sp *StatePrimer) Commit(ctx context.Context) error {
	// Create a proposal for the priming transaction
	prop, err := network.NewSignedProposal(
		sp.signer,
		sp.channel,
		sp.namespace,
		sp.nsVersion,
		[][]byte{[]byte("prime")},
		nil,
	)
	if err != nil {
		return err
	}

	inv, err := endorsement.Parse(prop, time.Time{})
	if err != nil {
		return err
	}

	// Collect endorsements from all builders
	var presps []*pb.ProposalResponse
	for _, builder := range sp.builders {
		presp, err := builder.Endorse(inv, endorsement.Success(sp.sim.Result(), nil, nil))
		if err != nil {
			return err
		}
		presps = append(presps, presp)
	}

	// Submit the endorsed transaction
	return sp.submitter.Submit(ctx, sdk.Endorsement{
		Responses: presps,
		Proposal:  inv.Proposal,
	})
}

// Reset creates a new simulation store, discarding all uncommitted changes.
func (sp *StatePrimer) Reset() (*StatePrimer, error) {
	sim, err := state.NewSimulationStore(context.TODO(), sp.db, sp.namespace, 0, sp.monotonicVersions)
	if err != nil {
		return nil, err
	}

	sp.sim = sim
	sp.stateDB = endorser.NewSnapshotDB(sim)
	return sp, nil
}

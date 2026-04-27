/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"math/rand"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	pb "github.com/hyperledger/fabric-protos-go-apiv2/peer"
	lc "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"github.com/hyperledger/fabric-x-sdk/network"
)

// StatePrimer provides a builder pattern for priming ledger state.
// It allows setting nonces, code, balances, and storage for addresses,
// then commits all changes in a single transaction.
type StatePrimer struct {
	gw                *core.Gateway
	db                endorser.ReadStore
	namespace         string
	signer            sdk.Signer
	builders          []endorsement.Builder
	channel           string
	nsVersion         string
	monotonicVersions bool

	// DualStateDB that tracks both Fabric and Ethereum state
	stateDB endorser.ExtendedStateDB

	priv *ecdsa.PrivateKey
}

// NewStatePrimer creates a new state primer builder.
func NewStatePrimer(
	gw *core.Gateway,
	db endorser.ReadStore,
	namespace string,
	signer sdk.Signer,
	builders []endorsement.Builder,
	channel string,
	nsVersion string,
	monotonicVersions bool,
) (*StatePrimer, error) {
	// Create a DualStateDB with both Fabric and Ethereum state tracking
	stateDB, err := endorser.NewStateDBWithDualState(context.TODO(), db, namespace, 0, monotonicVersions, nil)
	if err != nil {
		return nil, err
	}

	priv, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}

	return &StatePrimer{
		gw:                gw,
		db:                db,
		namespace:         namespace,
		signer:            signer,
		builders:          builders,
		channel:           channel,
		nsVersion:         nsVersion,
		monotonicVersions: monotonicVersions,
		stateDB:           stateDB,
		priv:              priv,
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

// AllocEntry represents a single account entry in a genesis allocation JSON file.
type AllocEntry struct {
	Balance string            `json:"balance"`
	Code    string            `json:"code"`
	Nonce   string            `json:"nonce"`
	Storage map[string]string `json:"storage"`
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
func (sp *StatePrimer) Commit(ctx context.Context, wait bool) error {
	// create a fake ethereum tx so we can use it to track priming
	tx, ethTxBytes, err := sp.fakeEthTx()
	if err != nil {
		return err
	}

	// Create a proposal for the priming transaction
	prop, err := network.NewSignedProposal(
		sp.signer,
		sp.channel,
		sp.namespace,
		sp.nsVersion,
		[][]byte{{byte(lc.ProposalTypeEVMTx)}, ethTxBytes},
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
		presp, err := builder.Endorse(inv, endorsement.Success(sp.stateDB.Result(), nil, nil))
		if err != nil {
			return err
		}
		presps = append(presps, presp)
	}

	return sp.commitAndWait(sdk.Endorsement{
		Responses: presps,
		Proposal:  inv.Proposal,
	}, tx, wait)
}

// Writes returns the ReadWriteSet of all state changes recorded since the last Reset.
// Safe to call after Commit — the StateDB is not cleared by Commit.
func (sp *StatePrimer) Writes() blocks.ReadWriteSet {
	return sp.stateDB.Result()
}

// Reset creates a new DualStateDB, discarding all uncommitted changes.
func (sp *StatePrimer) Reset() (*StatePrimer, error) {
	stateDB, err := endorser.NewStateDBWithDualState(context.TODO(), sp.db, sp.namespace, 0, sp.monotonicVersions, nil)
	if err != nil {
		return nil, err
	}

	sp.stateDB = stateDB
	return sp, nil
}

// GetEthStateDB extracts the ethStateDB from the underlying DualStateDB.
// This allows the primed state to be reused in subsequent operations.
func (sp *StatePrimer) GetEthStateDB() *ethstate.StateDB {
	if dualDB, ok := sp.stateDB.(*endorser.DualStateDB); ok {
		return dualDB.EthStateDB()
	}
	return nil
}

func (sp *StatePrimer) fakeEthTx() (*types.Transaction, []byte, error) {
	b := make([]byte, 16)
	//lint:ignore SA1019 intentional use of math/rand.Read, this is only for tests
	_, _ = rand.Read(b)
	tx := types.NewTx(&types.LegacyTx{
		Nonce: rand.Uint64(),
		To:    nil,
		Data:  b,
	})

	chainID, err := sp.gw.ChainID(context.TODO())
	if err != nil {
		return nil, nil, err
	}
	ethSigner := types.LatestSignerForChainID(chainID)

	signedTx, err := types.SignTx(tx, ethSigner, sp.priv)
	if err != nil {
		return nil, nil, err
	}

	ethTxBytes, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}

	return signedTx, ethTxBytes, nil
}

func (sp *StatePrimer) commitAndWait(end sdk.Endorsement, tx *types.Transaction, wait bool) error {
	if err := sp.gw.SubmitFabricTx(context.Background(), end); err != nil {
		return err
	}

	ec, err := NewNativeEthClient(sp.gw)
	if err != nil {
		return err
	}

	if wait {
		waitForCommit(context.Background(), ec, tx)
	}

	return nil
}

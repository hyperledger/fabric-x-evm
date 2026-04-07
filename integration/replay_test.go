/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/integration/contracts"
	"github.com/hyperledger/fabric-x-evm/utils"
	_ "modernc.org/sqlite"
)

type ERC20TxInfo struct {
	Op       string            // transfer / transferFrom / approve / other
	From     ethCommon.Address // tx signer (msg.sender)
	To       ethCommon.Address // tx.To() (contract address)
	Sender   ethCommon.Address // who loses tokens
	Receiver ethCommon.Address // who gains tokens
	Amount   *big.Int
	TxValue  *big.Int
	DataLen  int
}

func signerForTx(tx *types.Transaction) types.Signer {
	if tx.Type() != types.LegacyTxType {
		// Typed txs (EIP-2930, EIP-1559, etc.)
		return types.LatestSignerForChainID(tx.ChainId())
	}

	// Legacy tx
	if tx.Protected() {
		// EIP-155 legacy
		return types.NewEIP155Signer(tx.ChainId())
	}

	// Pre-EIP-155 legacy
	return types.HomesteadSigner{}
}

func parseERC20Tx(t *testing.T, raw []byte, erc20ABI abi.ABI) (*ERC20TxInfo, error) {
	var tx types.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, err
	}

	signer := signerForTx(&tx)
	from, err := types.Sender(signer, &tx)
	if err != nil {
		return nil, err
	}

	info := &ERC20TxInfo{
		Op:      "other",
		From:    from,
		TxValue: tx.Value(),
		DataLen: len(tx.Data()),
	}

	if tx.To() != nil {
		info.To = *tx.To()
	}

	// No calldata → not an ERC20 call
	if len(tx.Data()) < 4 {
		return info, nil
	}

	method, err := erc20ABI.MethodById(tx.Data()[:4])
	if err != nil {
		selector := hex.EncodeToString(tx.Data()[:4])
		t.Logf("unknown selector: 0x%s", selector)
		return info, nil
	}

	args, err := method.Inputs.Unpack(tx.Data()[4:])
	if err != nil {
		return info, nil
	}

	info.Op = method.Name

	switch method.Name {
	case "transfer":
		info.Receiver = args[0].(ethCommon.Address)
		info.Amount = args[1].(*big.Int)
		info.Sender = from

	case "approve":
		info.Receiver = args[0].(ethCommon.Address)
		info.Amount = args[1].(*big.Int)
		info.Sender = from

	case "transferFrom":
		info.Sender = args[0].(ethCommon.Address)
		info.Receiver = args[1].(ethCommon.Address)
		info.Amount = args[2].(*big.Int)
	}

	return info, nil
}

func testTetherTokenReplay(t *testing.T, th *TestHarness) {
	node1 := th.gateways[0]

	// --- Load deployment + transactions JSON ---
	jsonPath := "../testdata/tether_txs.json"
	t.Logf("Loading %s", jsonPath)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read json: %v", err)
	}

	type TxRecordRaw struct {
		Raw         string `json:"raw"`
		BlockNumber string `json:"block_number"`
		BlockTime   uint64 `json:"block_time"`
	}

	type ContractTxsLocal struct {
		Contract              string        `json:"contract"`
		DeploymentTxRaw       string        `json:"deployment_tx_raw"`
		DeploymentBlockNumber string        `json:"deployment_block_number"`
		DeploymentBlockTime   uint64        `json:"deployment_block_time"`
		Txs                   []TxRecordRaw `json:"txs"`
	}

	var ct ContractTxsLocal
	if err := json.Unmarshal(data, &ct); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}

	if ct.DeploymentTxRaw == "" {
		t.Fatal("JSON must include deployment transaction")
	}

	if len(ct.Txs) == 0 {
		t.Fatal("JSON must include at least one transaction to replay")
	}

	// --- Decode deployment tx ---
	deployRaw, err := hex.DecodeString(strings.TrimPrefix(ct.DeploymentTxRaw, "0x"))
	if err != nil {
		t.Fatalf("decode deployment raw hex: %v", err)
	}

	blockNum := new(big.Int)
	if ct.DeploymentBlockNumber != "" {
		if _, ok := blockNum.SetString(ct.DeploymentBlockNumber, 10); !ok {
			t.Fatalf("invalid deployment block number: %s", ct.DeploymentBlockNumber)
		}
	}

	// Create transaction object
	tx := new(types.Transaction)

	// Decode RLP into tx
	if err := tx.UnmarshalBinary(deployRaw); err != nil {
		t.Fatal(err)
	}

	// Get the sender address to prime balance
	signer := types.LatestSignerForChainID(tx.ChainId())
	from, err := types.Sender(signer, tx)
	if err != nil {
		t.Fatal(err)
	}

	// Prime the balance for the transaction sender
	// In a permissioned blockchain, we don't require pre-funded accounts
	// Calculate required balance: gas * gasPrice + value
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = big.NewInt(0)
	}
	gasCost := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), gasPrice)
	requiredBalance := new(big.Int).Add(gasCost, tx.Value())

	// Add some extra to be safe (e.g., 10 ETH extra)
	extraBalance := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
	totalBalance := new(big.Int).Add(requiredBalance, extraBalance)

	primer, err := th.NewStatePrimer()
	if err != nil {
		t.Fatalf("failed to create state primer: %v", err)
	}
	if err := primer.SetBalance(from, totalBalance).Commit(t.Context()); err != nil {
		t.Fatalf("failed to prime balance: %v", err)
	}

	t.Logf("Replaying deployment tx (block %s, time %d)", blockNum.String(), ct.DeploymentBlockTime)
	processCommon(t, node1, true, decodeRawTransactionT(t, deployRaw), &utils.BlockInfo{
		BlockNumber: blockNum,
		BlockTime:   ct.DeploymentBlockTime,
		GasLimit:    5_000_000,
	})

	contractAddr := crypto.CreateAddress(from, tx.Nonce())
	client, err := NewEthClient(contracts.TetherTokenMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatalf("NewEthClient failed: %v", err)
	}

	query := func(method string, params ...any) []any {
		return querySmartContract(t, node1, client, contractAddr, method, params...)
	}

	// --- Get totalSupply and deployment sender ---
	totalSupplyRes := query("totalSupply")
	if len(totalSupplyRes) == 0 {
		t.Fatalf("totalSupply query empty after deployment")
	}
	totalSupply := totalSupplyRes[0].(*big.Int)
	t.Logf("Initial totalSupply: %s", totalSupply.String())

	deployTx := new(types.Transaction)
	if err := deployTx.UnmarshalBinary(deployRaw); err != nil {
		t.Fatalf("unmarshal deployment tx: %v", err)
	}
	deployFrom, err := types.Sender(types.LatestSignerForChainID(deployTx.ChainId()), deployTx)
	if err != nil {
		t.Fatalf("get deploy sender: %v", err)
	}
	t.Logf("Deployment sender (initial minter): %s", deployFrom.Hex())

	balRes := query("balanceOf", deployFrom)
	if len(balRes) == 0 {
		t.Fatalf("balanceOf(deployFrom) query empty after deployment")
	}
	deployFromBal := balRes[0].(*big.Int)
	if deployFromBal.Cmp(totalSupply) != 0 {
		t.Fatalf("deployer balance %s does not match totalSupply %s", deployFromBal.String(), totalSupply.String())
	}

	// --- Seed replay ledger with validation ---
	balances := map[ethCommon.Address]*big.Int{}
	updateBalance := func(addr ethCommon.Address, delta *big.Int) {
		if addr == (ethCommon.Address{}) {
			return
		}

		// Get current balance (0 if not present)
		cur := new(big.Int)
		if b, ok := balances[addr]; ok {
			cur.Set(b)
		}

		// Compute new balance
		newBal := new(big.Int).Add(cur, delta)

		// Validate new balance is non-negative
		if newBal.Sign() < 0 {
			panic(fmt.Sprintf("replay ledger: balance for %s would go negative: cur=%s delta=%s", addr.Hex(), cur.String(), delta.String()))
		}

		balances[addr] = newBal
	}

	// Seed initial minter balance from deployment
	updateBalance(deployFrom, new(big.Int).Set(totalSupply))

	// --- Replay all transactions ---
	for i, txRec := range ct.Txs {
		t.Logf("Replaying tx #%d hash (block %s, time %d)", i+1, txRec.BlockNumber, txRec.BlockTime)

		rawTx, err := hex.DecodeString(strings.TrimPrefix(txRec.Raw, "0x"))
		if err != nil {
			t.Fatalf("decode raw tx #%d: %v", i+1, err)
		}

		txObj := new(types.Transaction)
		if err := txObj.UnmarshalBinary(rawTx); err != nil {
			t.Fatalf("unmarshal tx #%d: %v", i+1, err)
		}

		txBlockNum := new(big.Int)
		if txRec.BlockNumber != "" {
			if _, ok := txBlockNum.SetString(txRec.BlockNumber, 10); !ok {
				t.Fatalf("invalid block number for tx #%d: %s", i+1, txRec.BlockNumber)
			}
		}

		info, err := parseERC20Tx(t, rawTx, *client.abi)
		if err != nil {
			t.Fatalf("parseERC20Tx failed for tx #%d: %v", i+1, err)
		}

		// compute expected deltas
		if info.Op == "transfer" || info.Op == "transferFrom" {
			amt := new(big.Int)
			if info.Amount != nil {
				amt.Set(info.Amount)
			}

			if info.Sender == (ethCommon.Address{}) {
				// mint
				updateBalance(info.Receiver, amt)
			} else {
				// normal transfer
				updateBalance(info.Sender, new(big.Int).Neg(amt))
				updateBalance(info.Receiver, amt)
			}
		}

		// Prime the nonce for the transaction sender before replaying
		txSender, err := types.Sender(signerForTx(txObj), txObj)
		if err != nil {
			t.Fatalf("get tx sender for tx #%d: %v", i+1, err)
		}
		primer, err := th.NewStatePrimer()
		if err != nil {
			t.Fatalf("create state primer for tx #%d: %v", i+1, err)
		}
		if err := primer.SetNonce(txSender, txObj.Nonce()).Commit(t.Context()); err != nil {
			t.Fatalf("prime nonce for tx #%d: %v", i+1, err)
		}

		// Replay tx
		processCommon(t, node1, true, decodeRawTransactionT(t, rawTx), &utils.BlockInfo{
			BlockNumber: txBlockNum,
			BlockTime:   txRec.BlockTime,
			GasLimit:    5_000_000,
		})
	}

	// --- Verify all balances on-chain ---
	for addr, expected := range balances {
		res := query("balanceOf", addr)
		if len(res) == 0 {
			t.Errorf("balanceOf(%s) returned empty", addr.Hex())
			continue
		}
		onchain := res[0].(*big.Int)
		if onchain.Cmp(expected) != 0 {
			t.Errorf("balance mismatch for %s: expected %s, got %s", addr.Hex(), expected.String(), onchain.String())
		} else {
			t.Logf("balance match for %s: %s", addr.Hex(), onchain.String())
		}
	}

	t.Log("Replay complete, all balances verified against expected deltas")
}

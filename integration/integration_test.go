/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"fmt"
	"io"
	"math/big"
	"os"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/tests"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/integration/contracts"
	"google.golang.org/grpc/grpclog"
	_ "modernc.org/sqlite"
)

const (
	Namespace = "basic"
)

type testCase struct {
	name        string
	nodes       int
	fn          func(*testing.T, *TestHarness)
	fork        string
	overrides   map[string]any
	primeDbPath string
}

var cases = []testCase{
	{
		name:  "greeter",
		fn:    testGreeter,
		nodes: 2,
	},
	{
		name:  "counter",
		fn:    testCounter,
		nodes: 2,
	},
	{
		name:  "tether_token",
		fn:    testTetherToken,
		nodes: 2,
	},
	{
		name:  "tether_token_parallel",
		fn:    testTetherTokenParallel,
		nodes: 2,
	},
	{
		name:        "tether_token_replay",
		fn:          testTetherTokenReplay,
		nodes:       2,
		fork:        "Byzantium",                                 // replaying against the latest forks causes out of gas error
		overrides:   map[string]any{"Network.ChainID": int64(1)}, // mainnet chain ID which was used in the signature
		primeDbPath: "../testdata/alloc_tether_replay.json",
	},
	{
		name:  "uniswap_factory",
		fn:    testUniswapFactory,
		nodes: 2,
	},
	{
		name:  "nonce_validation",
		fn:    testNonceValidation,
		nodes: 2,
	},
}

// TestLocal tests a good portion of the fabric functionality, with the difference that
// it doesn't require fabric-x to be running. After endorsement, instead of submitting it to
// the orderer, it parses the endorsement back to a read/write set and stores it directly in
// the endorser database. So the transaction data goes, instead of:
// Gateway (receive) -> Endorser (endorse) -> Gateway (submit) -> Orderer (order) -> Peer (commit) -> Endorser (update state)
// Gateway (receive) -> Endorser (endorse) -> Gateway (update state)
func TestLocal(t *testing.T) {
	// silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, err := NewLocalTestHarness(t, TestLogger{T: t}, evmConfig(tc.fork), tc.primeDbPath, "fabric", tc.overrides)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { th.Stop() })
			tc.fn(t, th)
		})
	}
}

// TestLocalX is TestLocal but with fabric-x encoding of transactions
func TestLocalX(t *testing.T) {
	// silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, err := NewLocalTestHarness(t, TestLogger{T: t}, evmConfig(tc.fork), tc.primeDbPath, "fabric-x", tc.overrides)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { th.Stop() })
			tc.fn(t, th)
		})
	}
}

// TestFabric requires the fabric samples network to be running.
func TestFabric(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Fabric test in short mode")
	}
	// silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, err := newFabricTestHarness(t, TestLogger{T: t}, evmConfig(tc.fork), tc.primeDbPath, tc.overrides)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { th.Stop() })
			tc.fn(t, th)
		})
	}
}

// TestFabricX requires the fabric-x committer to be running.
func TestFabricX(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping FabricX test in short mode")
	}
	// silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, err := NewFabricXTestHarness(t, TestLogger{T: t}, evmConfig(tc.fork), tc.primeDbPath, tc.overrides)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { th.Stop() })
			tc.fn(t, th)
		})
	}
}

// evmConfig returns an empty EVMConfig, or, if the name of an ethereum fork
// is provided, an EVMConfig with that fork as ChainConfig. This can be used
// for replaying historic ethereum transactions from older forks.
func evmConfig(fork string) endorser.EVMConfig {
	if len(fork) == 0 {
		return endorser.EVMConfig{}
	}
	c, _, err := tests.GetChainConfig(fork)
	if err != nil {
		fmt.Println(err)
	}

	return endorser.EVMConfig{ChainConfig: c}
}

func testGreeter(t *testing.T, th *TestHarness) {
	node1 := th.Gateways[0]
	node2 := th.Gateways[0] // TODO: do we need two?

	ethA, err := NewEthClient(contracts.HelloMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	// deploy contract
	addr := deploySmartContract(t, node1, ethA)

	// test body
	firstGreeting := "Hello"
	callSmartContract(t, ethA, addr, node2, "setGreeting", nil, firstGreeting)

	querySmartContractExpect(t, ethA, addr, th, firstGreeting, "greet")

	secondGreeting := "Hi 👋 this is a greeting with a special character and more bytes than can fit in a single slot"
	env := getEndorsedTxForSmartContractCall(t, ethA, addr, node2, "setGreeting", nil, secondGreeting)

	// not committed yet, expect to still be Hello
	querySmartContractExpect(t, ethA, addr, th, firstGreeting, "greet")

	submit(t, node1, env)

	querySmartContractExpect(t, ethA, addr, th, secondGreeting, "greet")
}

func testCounter(t *testing.T, th *TestHarness) {
	node1 := th.Gateways[0]
	node2 := th.Gateways[0] // TODO: do we need two?

	ethOwner, err := NewEthClient(contracts.CounterMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	// deploy contract
	addr := deploySmartContract(t, node1, ethOwner)

	// test body
	x := big.NewInt(0)
	x.Add(x, big.NewInt(1))

	callSmartContract(t, ethOwner, addr, node2, "increment", nil)

	querySmartContractExpect(t, ethOwner, addr, th, x, "getCount")

	x.Sub(x, big.NewInt(1))

	callSmartContract(t, ethOwner, addr, node1, "decrement", nil)

	querySmartContractExpect(t, ethOwner, addr, th, x, "getCount")

	env := getEndorsedTxForSmartContractCall(t, ethOwner, addr, node2, "increment", nil)
	querySmartContractExpect(t, ethOwner, addr, th, x, "getCount")

	x.Add(x, big.NewInt(1))
	submit(t, node2, env)

	querySmartContractExpect(t, ethOwner, addr, th, x, "getCount")
}

func testNonceValidation(t *testing.T, th *TestHarness) {
	node := th.Gateways[0]
	ethClient, err := NewEthClient(contracts.CounterMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	// Deploy counter contract
	addr := deploySmartContract(t, node, ethClient)

	// Prime the nonce to 3 for the client's address
	clientAddr := ethClient.Address()
	primer, err := th.NewStatePrimer()
	if err != nil {
		t.Fatalf("failed to create state primer: %v", err)
	}
	if err := primer.SetNonce(clientAddr, 3).Commit(t.Context(), true); err != nil {
		t.Fatalf("failed to prime nonce: %v", err)
	}

	// get the transaction with nonce equal to 3
	tx, err := ethClient.TxForCall(t.Context(), node, &addr, "increment", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Prime the nonce to 5 for the client's address now
	primer, err = th.NewStatePrimer()
	if err != nil {
		t.Fatalf("failed to create state primer: %v", err)
	}
	if err := primer.SetNonce(clientAddr, 5).Commit(t.Context(), true); err != nil {
		t.Fatalf("failed to prime nonce: %v", err)
	}

	// call should fail now - transaction has nonce 3 but ledger has nonce 5
	_, err = node.ExecuteEthTx(t.Context(), tx, nil)
	if err == nil {
		t.Fatal("expected transaction with wrong nonce to fail, but it succeeded")
	}
	expectedErr := "process EVM transaction: nonce too low"
	if err.Error() != expectedErr {
		t.Fatalf("expected error %q, got %q", expectedErr, err.Error())
	}
	t.Logf("Transaction with wrong nonce correctly failed: %v", err)

	// Now call with correct nonce (5) - should succeed
	callSmartContract(t, ethClient, addr, node, "increment", nil)
	querySmartContractExpect(t, ethClient, addr, th, big.NewInt(1), "getCount")
}

func testTetherToken(t *testing.T, th *TestHarness) {
	node1 := th.Gateways[0]
	node2 := th.Gateways[0] // TODO: do we need two?
	ethOwner, err := NewEthClient(contracts.TetherTokenMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}
	ethA, err := NewEthClient(contracts.TetherTokenMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	// -------------------- deploy contract --------------------
	deploySupply := big.NewInt(100_000_000_000)
	tokenName := "Tether USD"
	tokenSymbol := "USDT"
	tokenDecimals := big.NewInt(6)

	// Deploy contract
	addr := deploySmartContract(t, node1, ethOwner, deploySupply, tokenName, tokenSymbol, tokenDecimals)

	// -------------------- full test body --------------------

	// Initial validations
	querySmartContractExpect(t, ethA, addr, th, deploySupply, "totalSupply")
	querySmartContractExpect(t, ethA, addr, th, tokenName, "name")
	querySmartContractExpect(t, ethA, addr, th, tokenSymbol, "symbol")
	querySmartContractExpect(t, ethA, addr, th, tokenDecimals, "decimals")
	querySmartContractExpect(t, ethA, addr, th, deploySupply, "balanceOf", ethOwner.Address())

	// -------------------- Owner transfers to user1 --------------------
	toA := big.NewInt(500_000)
	callSmartContract(t, ethOwner, addr, node2, "transfer", nil, ethA.Address(), toA)

	ownerBalance := new(big.Int).Sub(deploySupply, toA)
	querySmartContractExpect(t, ethOwner, addr, th, ownerBalance, "balanceOf", ethOwner.Address())
	querySmartContractExpect(t, ethOwner, addr, th, toA, "balanceOf", ethA.Address())

	// -------------------- Owner approves user2 --------------------
	approveAmount := big.NewInt(100_000)
	callSmartContract(t, ethOwner, addr, node2, "approve", nil, ethA.Address(), approveAmount)
	querySmartContractExpect(t, ethOwner, addr, th, approveAmount, "allowance", ethOwner.Address(), ethA.Address())

	// -------------------- User2 transferFrom --------------------
	callSmartContract(t, ethA, addr, node2, "transferFrom", nil, ethOwner.Address(), ethA.Address(), approveAmount)
	ownerBalance.Sub(ownerBalance, approveAmount)
	toA.Add(toA, approveAmount)
	zero := big.NewInt(0)
	querySmartContractExpect(t, ethOwner, addr, th, ownerBalance, "balanceOf", ethOwner.Address())
	querySmartContractExpect(t, ethOwner, addr, th, toA, "balanceOf", ethA.Address())
	querySmartContractExpect(t, ethOwner, addr, th, zero, "allowance", ethOwner.Address(), ethA.Address())

	// -------------------- Blacklist user1 --------------------
	callSmartContract(t, ethOwner, addr, node2, "addBlackList", nil, ethA.Address())
	trueVal := true
	querySmartContractExpect(t, ethA, addr, th, trueVal, "isBlackListed", ethA.Address())

	callSmartContract(t, ethOwner, addr, node2, "destroyBlackFunds", nil, ethA.Address())
	querySmartContractExpect(t, ethA, addr, th, zero, "balanceOf", ethA.Address())
	querySmartContractExpect(t, ethOwner, addr, th, zero, "balanceOf", ethA.Address())
	querySmartContractExpect(t, ethOwner, addr, th, ownerBalance, "balanceOf", ethOwner.Address())

	newTotalSupply := new(big.Int).Sub(deploySupply, toA)
	querySmartContractExpect(t, ethA, addr, th, zero, "balanceOf", ethA.Address())
	querySmartContractExpect(t, ethA, addr, th, newTotalSupply, "balanceOf", ethOwner.Address())
	querySmartContractExpect(t, ethA, addr, th, newTotalSupply, "totalSupply")

	callSmartContract(t, ethOwner, addr, node2, "removeBlackList", nil, ethA.Address())
	falseVal := false
	querySmartContractExpect(t, ethA, addr, th, falseVal, "isBlackListed", ethA.Address())

	// -------------------- Pause/unpause --------------------
	callSmartContract(t, ethOwner, addr, node2, "pause", nil)
	callSmartContract(t, ethOwner, addr, node2, "unpause", nil)

	// -------------------- Ownership transfer to user2 --------------------
	callSmartContract(t, ethOwner, addr, node2, "transferOwnership", nil, ethA.Address())
	querySmartContractExpect(t, ethA, addr, th, ethA.Address(), "getOwner")
	querySmartContractExpect(t, ethA, addr, th, zero, "balanceOf", ethA.Address())
	querySmartContractExpect(t, ethA, addr, th, newTotalSupply, "balanceOf", ethOwner.Address())
	querySmartContractExpect(t, ethA, addr, th, newTotalSupply, "totalSupply")

	// -------------------- User2 issues tokens --------------------
	issueAmount := big.NewInt(500_000)
	ownerAmount := new(big.Int).Set(newTotalSupply)
	totalAmount := new(big.Int).Add(issueAmount, ownerAmount)
	callSmartContract(t, ethA, addr, node2, "issue", nil, issueAmount)
	querySmartContractExpect(t, ethA, addr, th, issueAmount, "balanceOf", ethA.Address())
	querySmartContractExpect(t, ethA, addr, th, ownerAmount, "balanceOf", ethOwner.Address())
	querySmartContractExpect(t, ethA, addr, th, totalAmount, "totalSupply")

	// -------------------- User2 redeems tokens --------------------
	redeemAmount := big.NewInt(100_000)
	callSmartContract(t, ethA, addr, node2, "redeem", nil, redeemAmount)
	issueAmount.Sub(issueAmount, redeemAmount)
	totalAmount.Sub(totalAmount, redeemAmount)
	querySmartContractExpect(t, ethA, addr, th, issueAmount, "balanceOf", ethA.Address())
	querySmartContractExpect(t, ethA, addr, th, ownerAmount, "balanceOf", ethOwner.Address())
	querySmartContractExpect(t, ethA, addr, th, totalAmount, "totalSupply")
}

func testTetherTokenParallel(t *testing.T, th *TestHarness) {
	node1 := th.Gateways[0]
	node2 := th.Gateways[0] // TODO: do we need two?

	ethOwner, err := NewEthClient(contracts.TetherTokenMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}
	ethA, err := NewEthClient(contracts.TetherTokenMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}
	ethB, err := NewEthClient(contracts.TetherTokenMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}
	ethC, err := NewEthClient(contracts.TetherTokenMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}
	ethD, err := NewEthClient(contracts.TetherTokenMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	// -------------------- deploy contract --------------------
	deploySupply := big.NewInt(1_000_000)
	tokenName := "Tether"
	tokenSymbol := "USDT"
	tokenDecimals := big.NewInt(6)

	// Deploy contract
	addr := deploySmartContract(t, node2, ethOwner, deploySupply, tokenName, tokenSymbol, tokenDecimals)

	// -------------------- test body --------------------
	// Initial validations
	querySmartContractExpect(t, ethA, addr, th, deploySupply, "totalSupply")
	querySmartContractExpect(t, ethC, addr, th, tokenName, "name")
	querySmartContractExpect(t, ethA, addr, th, tokenSymbol, "symbol")
	querySmartContractExpect(t, ethC, addr, th, tokenDecimals, "decimals")
	querySmartContractExpect(t, ethA, addr, th, deploySupply, "balanceOf", ethOwner.Address())

	// Initial balances:
	//   Owner:  1,000,000 USDT
	//   User A:         0 USDT
	//   User B:         0 USDT
	//   User C:         0 USDT
	//   User D:         0 USDT

	// // Owner transfers 200,000 to userA
	toA := big.NewInt(200_000)
	callSmartContract(t, ethOwner, addr, node2, "transfer", nil, ethA.Address(), toA)

	// Result:
	//   Owner:  800,000 USDT (-200,000)
	//   User A: 200,000 USDT (+200,000)
	//   User B:       0 USDT
	//   User C:       0 USDT
	//   User D:       0 USDT

	ownerBalance := new(big.Int).Sub(deploySupply, toA)
	querySmartContractExpect(t, ethA, addr, th, ownerBalance, "balanceOf", ethOwner.Address())
	querySmartContractExpect(t, ethA, addr, th, toA, "balanceOf", ethA.Address())

	// Owner transfers 150,000 to userB
	toB := big.NewInt(150_000)
	callSmartContract(t, ethOwner, addr, node2, "transfer", nil, ethB.Address(), toB)

	// Result:
	//   Owner:  650,000 USDT (-150,000)
	//   User A: 200,000 USDT
	//   User B: 150,000 USDT (+150,000)
	//   User C:       0 USDT
	//   User D:       0 USDT

	ownerBalance = ownerBalance.Sub(ownerBalance, toB)
	querySmartContractExpect(t, ethA, addr, th, ownerBalance, "balanceOf", ethOwner.Address()) // 650,000
	querySmartContractExpect(t, ethA, addr, th, toB, "balanceOf", ethB.Address())              // 150,000

	// UserA transfers 30,000 USDT to UserC and
	// UserB transfers 30,000 USDT to UserD
	toAC := big.NewInt(30_000)
	toBD := big.NewInt(20_000)
	env1 := getEndorsedTxForSmartContractCall(t, ethA, addr, node1, "transfer", nil, ethC.Address(), toAC)
	env2 := getEndorsedTxForSmartContractCall(t, ethB, addr, node2, "transfer", nil, ethD.Address(), toBD)

	// Commit the two transactions in parallel (in one block)
	// Result:
	//   Owner:  65,0000 USDT
	//   User A: 170,000 USDT (-30,000)
	//   User B: 130,000 USDT (-20,000)
	//   User C:  30,000 USDT (+30,000)
	//   User D:  20,000 USDT (+20,000)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); submit(t, node1, env1) }()
	go func() { defer wg.Done(); submit(t, node2, env2) }()
	wg.Wait()

	toB = toB.Sub(toB, toBD)
	toA = toA.Sub(toA, toAC)
	querySmartContractExpect(t, ethA, addr, th, toA, "balanceOf", ethA.Address())  // 170,000
	querySmartContractExpect(t, ethA, addr, th, toB, "balanceOf", ethB.Address())  // 130,000
	querySmartContractExpect(t, ethA, addr, th, toAC, "balanceOf", ethC.Address()) //  30,000
	querySmartContractExpect(t, ethA, addr, th, toBD, "balanceOf", ethD.Address()) //  20,000
}

func testUniswapFactory(t *testing.T, th *TestHarness) {
	node := th.Gateways[0]

	// -------------------- clients --------------------
	erc20A, err := NewEthClient(contracts.GenericERC20MetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	erc20B, err := NewEthClient(contracts.GenericERC20MetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	factoryClient, err := NewEthClient(contracts.UniswapV2FactoryMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	pairClient, err := NewEthClient(contracts.UniswapV2PairMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	// -------------------- deploy ERC20 tokens --------------------
	initialSupply := big.NewInt(1_000_000)

	tokenAAddr := deploySmartContract(
		t,
		node,
		erc20A,
		"TokenA",
		"TKA",
		initialSupply,
		uint8(18),
	)

	tokenBAddr := deploySmartContract(
		t,
		node,
		erc20B,
		"TokenB",
		"TKB",
		initialSupply,
		uint8(18),
	)

	// -------------------- deploy Uniswap factory --------------------
	feeToSetter := erc20A.Address()

	factoryAddr := deploySmartContract(
		t,
		node,
		factoryClient,
		feeToSetter,
	)

	// -------------------- create pair --------------------
	callSmartContract(
		t,
		factoryClient,
		factoryAddr,
		node,
		"createPair",
		nil,
		tokenAAddr,
		tokenBAddr,
	)

	// -------------------- fetch pair address --------------------
	res := querySmartContract(
		t,
		node,
		factoryClient,
		factoryAddr,
		"getPair",
		tokenAAddr,
		tokenBAddr,
	)

	if len(res) != 1 {
		t.Fatalf("expected 1 return value, got %d", len(res))
	}

	pairAddr := res[0].(common.Address)

	if pairAddr == (common.Address{}) {
		t.Fatal("pair address is zero")
	}

	// Verify symmetric lookup
	querySmartContractExpect(
		t,
		factoryClient,
		factoryAddr,
		th,
		pairAddr,
		"getPair",
		tokenBAddr,
		tokenAAddr,
	)

	// -------------------- validate pair state --------------------
	token0 := querySmartContract(
		t,
		node,
		pairClient,
		pairAddr,
		"token0",
	)[0].(common.Address)

	token1 := querySmartContract(
		t,
		node,
		pairClient,
		pairAddr,
		"token1",
	)[0].(common.Address)

	// Uniswap sorts addresses lexicographically
	if token0 != tokenAAddr && token0 != tokenBAddr {
		t.Fatalf("unexpected token0: %s", token0.Hex())
	}
	if token1 != tokenAAddr && token1 != tokenBAddr {
		t.Fatalf("unexpected token1: %s", token1.Hex())
	}
	if token0 == token1 {
		t.Fatal("token0 and token1 must differ")
	}

	// -------------------- sanity: factory recorded pair --------------------
	allPairsLength := querySmartContract(
		t,
		node,
		factoryClient,
		factoryAddr,
		"allPairsLength",
	)[0].(*big.Int)

	if allPairsLength.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected 1 pair, got %s", allPairsLength.String())
	}

	// -------------------- approve tokens for pair --------------------
	liquidityProvider := erc20A.Address()

	amountA := big.NewInt(100_000)
	amountB := big.NewInt(200_000)

	// TokenA approve
	callSmartContract(
		t,
		erc20A,
		tokenAAddr,
		node,
		"approve",
		nil,
		pairAddr,
		amountA,
	)

	// TokenB approve
	callSmartContract(
		t,
		erc20B,
		tokenBAddr,
		node,
		"approve",
		nil,
		pairAddr,
		amountB,
	)

	// -------------------- transfer tokens into pair --------------------
	callSmartContract(
		t,
		erc20A,
		tokenAAddr,
		node,
		"transfer",
		nil,
		pairAddr,
		amountA,
	)

	callSmartContract(
		t,
		erc20B,
		tokenBAddr,
		node,
		"transfer",
		nil,
		pairAddr,
		amountB,
	)

	// -------------------- mint liquidity --------------------
	callSmartContract(
		t,
		pairClient,
		pairAddr,
		node,
		"mint",
		nil,
		liquidityProvider,
	)

	// -------------------- validate reserves --------------------
	reserves := querySmartContract(
		t,
		node,
		pairClient,
		pairAddr,
		"getReserves",
	)

	reserve0 := reserves[0].(*big.Int)
	reserve1 := reserves[1].(*big.Int)

	if reserve0.Sign() == 0 || reserve1.Sign() == 0 {
		t.Fatal("expected non-zero reserves after mint")
	}

	// -------------------- swap --------------------
	swapIn := big.NewInt(10_000)

	callSmartContract(
		t,
		erc20A,
		tokenAAddr,
		node,
		"transfer",
		nil,
		pairAddr,
		swapIn,
	)

	var amount0Out = big.NewInt(0)
	var amount1Out = big.NewInt(0)

	// Determine direction
	if token0 == tokenAAddr {
		amount1Out = big.NewInt(1) // minimal output, invariant-safe
	} else {
		amount0Out = big.NewInt(1)
	}

	callSmartContract(
		t,
		pairClient,
		pairAddr,
		node,
		"swap",
		nil,
		amount0Out,
		amount1Out,
		liquidityProvider,
		[]byte{},
	)

	// -------------------- validate reserves changed --------------------
	reservesAfter := querySmartContract(
		t,
		node,
		pairClient,
		pairAddr,
		"getReserves",
	)

	reserve0After := reservesAfter[0].(*big.Int)
	reserve1After := reservesAfter[1].(*big.Int)

	if reserve0After.Cmp(reserve0) == 0 && reserve1After.Cmp(reserve1) == 0 {
		t.Fatal("expected reserves to change after swap")
	}
}

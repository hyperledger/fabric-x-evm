# JSON-RPC Error Codes

This document specifies the JSON-RPC error contract the gateway exposes. It is the
counterpart to the geth error contract — Ethereum clients (ethers.js, web3.py, native
`ethclient`, MetaMask, Hardhat) branch on these codes, so any drift surfaces as wallet
or library breakage on the user side.

The single source of truth in code is [`gateway/api/rpcerr`](../gateway/api/rpcerr/rpcerr.go).

## Code reference

| Code | Name | When the gateway returns it |
|-----:|------|------|
| `-32602` | Invalid params | Malformed caller input — bad raw-tx bytes, unparseable hex args (`from`, `gas`, `gasPrice`, `value`, `input`/`data`, `maxFeePerGas`, `maxPriorityFeePerGas`). |
| `-32603` | Internal error | Unexpected backend failure: state-lookup fault (e.g. nonce lookup), endorser unreachable, or any error that has no more specific classification. |
| `-32003` | Transaction rejected | Pre-flight validation rule violation: nonce too low, intrinsic gas, insufficient funds, unsupported tx type, init-code size exceeded (EIP-3860), invalid sender, unprotected (non-EIP-155) tx. |
| `-32000` | Execution reverted | EVM revert during `eth_call`. The `data` field carries the raw revert payload, hex-encoded; the `message` field carries the formatted reason (`"execution reverted: <abi reason>"` or just `"execution reverted"` when the payload cannot be ABI-decoded). |

## Mapping by RPC method

### `eth_sendRawTransaction`

| Cause | Code |
|------|-----:|
| `tx.UnmarshalBinary` fails | `-32602` |
| `core.ErrNonceTooLow` / `ErrNonceTooHigh` | `-32003` |
| `core.ErrIntrinsicGas` | `-32003` |
| `core.ErrInsufficientFunds` | `-32003` |
| `core.ErrTxTypeNotSupported` | `-32003` |
| `core.ErrMaxInitCodeSizeExceeded` | `-32003` |
| `txpool.ErrInvalidSender` | `-32003` |
| `domain.ErrUnprotectedTx` (non-EIP-155) | `-32003` |
| `domain.ErrNonceLookup` (state lookup failure) | `-32603` |
| Any other error | `-32603` |

### `eth_call`

| Cause | Code |
|------|-----:|
| Bad hex in `argsToCallMsg` | `-32602` |
| EVM revert (`*domain.RevertError`) | `-32000` (with revert payload as `data`) |
| Endorser / backend failure | `-32603` |

### Read-side methods (`eth_getBalance`, `eth_getCode`, `eth_getStorageAt`, `eth_getTransactionCount`, etc.)

Backend errors flow through unchanged today and surface as `-32603`. A future PR
may classify state-lookup failures distinctly; until then, treat any error from a
read-side method as `Internal`.

## Examples

### Reverted `eth_call`

Standard Ethereum nodes return:

```json
{"code": 3, "message": "execution reverted", "data": "0x08c379a0..."}
```

The gateway returns:

```json
{"code": -32000, "message": "execution reverted: ERC20: insufficient allowance", "data": "0x08c379a0..."}
```

The `code` differs (`3` vs `-32000`) because geth's own `rpc.Server` emits the
`-32000`/`-32099` server-defined range; libraries that treat any `data`-bearing
error as a revert (`ethers.js`, OpenZeppelin's `_callOptionalReturn`, Hardhat's
`expectRevert`) work unchanged. The `data` payload is the raw revert bytes — exactly
what custom-error decoders (`error Foo(uint amount)`) need.

### Nonce too low on `eth_sendRawTransaction`

```json
{"code": -32003, "message": "nonce too low: address 0x... current nonce 5, tx nonce 1"}
```

Wallets that branch on `UNPREDICTABLE_GAS_LIMIT` / `NONCE_EXPIRED` (ethers.js v5)
or on the `-32003` code (web3.py) will surface the failure to the user as a tx
rejection rather than a network error.

### Bad hex in call args

```json
{"code": -32602, "message": "invalid gas: hex string without 0x prefix"}
```

## Layering

The classifier lives in [`gateway/api/rpcerrors.go`](../gateway/api/rpcerrors.go).
It depends on:

- [`gateway/api/rpcerr`](../gateway/api/rpcerr/) — typed RPC error constructors
- [`gateway/domain/errors.go`](../gateway/domain/errors.go) — sentinel types
  (`ErrUnprotectedTx`, `ErrNonceLookup`, `RevertError` / `ErrExecutionReverted`)

The `gateway/api` package never imports `gateway/core`. Core code raises typed
errors via `gateway/domain`; the api layer translates them to `rpcerr.*` codes
at the RPC boundary.

## Tests that lock the contract

- [`gateway/api/rpcerr/rpcerr_test.go`](../gateway/api/rpcerr/rpcerr_test.go) —
  `TestStandardCodesMatchSpec` asserts the four code values; CI fails if any drifts.
- [`gateway/api/rpcerrors_test.go`](../gateway/api/rpcerrors_test.go) — covers
  `classifyValidationError` and `classifyCallError` per sentinel.
- [`gateway/api/eth_test.go`](../gateway/api/eth_test.go) — `eth_call` and
  `eth_sendRawTransaction` end-to-end via `stubBackend`.
- [`integration/integration_test.go`](../integration/integration_test.go) —
  `testRevertHandling` exercises both revert paths (eth_call and send) against
  TetherToken via `NewNativeEthClient`.

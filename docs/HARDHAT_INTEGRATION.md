# Hardhat Integration with OpenZeppelin Contracts

This document describes how to run OpenZeppelin's Hardhat test suite against fabric-evm to validate Ethereum compatibility.

## Overview

The Hardhat integration allows you to test fabric-evm using real-world smart contract test suites from the OpenZeppelin contracts library. This provides a comprehensive validation of EVM compatibility beyond custom test cases.

## Architecture

### Components

1. **Test RPC Server** (`gateway/testimpl/`)
   - Extends the production RPC server with test-only methods
   - Implements `eth_accounts` and `eth_sendTransaction` with server-side signing
   - Provides Hardhat helper methods (`hardhat_*`, `evm_*`)
   - **WARNING**: Server-side signing is UNSAFE for production

2. **Test Accounts** (`testdata/test_accounts.json`)
   - Pre-configured accounts matching Hardhat's default accounts
   - Private keys available for server-side transaction signing
   - Used by `eth_accounts` and `eth_sendTransaction`

3. **Hardhat Configuration** (`testdata/openzeppelin-contracts/hardhat.config.js`)
   - Configured with `fabricevm` network pointing to `http://127.0.0.1:8545`
   - Chain ID: 31337 (Hardhat default)
   - EVM version: Cancun (for modern opcode support)

4. **Test Harness** (`scripts/run_hardhat_test.sh`)
   - Automated script to run Hardhat tests
   - Manages Fabric network, gateway, and test execution
   - Handles cleanup on exit

## Prerequisites

- Node.js and npm
- Go 1.21+
- Docker (for Fabric network)
- Git (for submodules)

## Quick Start

### 1. Initialize OpenZeppelin Submodule

```bash
git submodule update --init --recursive testdata/openzeppelin-contracts
cd testdata/openzeppelin-contracts
npm install
cd ../..
```

### 2. Run ERC20 Tests

```bash
make hardhat-tests
```

This will:
1. Start the Fabric network
2. Start the gateway with test RPC enabled
3. Run OpenZeppelin ERC20 tests
4. Clean up automatically

### 3. Run Custom Tests

To run a different test file:

```bash
./scripts/run_hardhat_test.sh test/token/ERC721/ERC721.test.js
```

## Test RPC Methods

### Standard Methods (with test accounts)

- **`eth_accounts`**: Returns configured test account addresses
- **`eth_sendTransaction`**: Signs and sends transactions server-side using test account private keys

### Hardhat Helper Methods

- **`hardhat_setCode`**: Set code at an address (stub implementation)
- **`evm_snapshot`**: Create a state snapshot (stub implementation)
- **`evm_revert`**: Revert to a previous snapshot (stub implementation)
- **`evm_mine`**: Mine a new block (stub - blocks created by Fabric consensus)
- **`evm_increaseTime`**: Increase next block timestamp (stub)
- **`evm_setNextBlockTimestamp`**: Set next block timestamp (stub)

**Note**: Snapshot/revert methods are currently stubs that return success but don't actually preserve/restore state. This is sufficient for many tests but may cause failures in tests that rely on state rollback.

## Configuration

### Gateway Startup

The gateway must be started with test RPC enabled:

```bash
cd gateway
go run . --protocol fabric \
    --test-accounts-path ../testdata/test_accounts.json \
    --enable-test-rpc
```

### Test Accounts

Test accounts are defined in `testdata/test_accounts.json`:

```json
{
  "accounts": [
    {
      "address": "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
      "privateKey": "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
    }
    // ... more accounts
  ]
}
```

These match Hardhat's default test accounts for compatibility.

## Compatibility Status

### ✅ Implemented

- Test account management
- Server-side transaction signing
- Hardhat network detection (client version string)
- Basic Hardhat helper method stubs
- Genesis block support (nullable parent_hash)
- Modern EVM opcodes (PUSH0, etc.) - via PR #105

### ⚠️ Partial / Stub Implementation

- **Snapshot/Revert**: Methods exist but don't actually preserve/restore state
- **Block manipulation**: Time/mining methods are stubs
- **Code modification**: `hardhat_setCode` is a stub

### 🔄 Future Enhancements

1. **Full snapshot/revert implementation**
   - Use `StateDB.Copy()` to create real snapshots
   - Maintain snapshot stack in memory
   - Implement proper state restoration

2. **Block timestamp control**
   - Allow tests to control block timestamps
   - Integrate with Fabric block creation

3. **Enhanced debugging**
   - Add `hardhat_impersonateAccount`
   - Add `hardhat_stopImpersonatingAccount`

## Security Warnings

⚠️ **CRITICAL**: The test RPC server performs server-side transaction signing, which is inherently insecure:

- Private keys are stored on the server
- Anyone with RPC access can sign transactions as any test account
- **NEVER** use test RPC mode in production
- **NEVER** use real accounts/keys in test account configuration

The test RPC is designed ONLY for:
- Local development
- Automated testing
- CI/CD pipelines in isolated environments

## Troubleshooting

### Tests timeout during "before each" hook (40 seconds)

**Symptom**: Tests fail with `Error: Timeout of 40000ms exceeded. For async tests and hooks, ensure "done()" is called`

**Root Cause**: The gateway is not started with test RPC methods enabled. Without `eth_accounts` and `eth_sendTransaction`, Hardhat hangs waiting for responses.

**Solution**: Ensure the gateway is started with the `--enable-test-rpc` flag:

```bash
go run ./cmd/fxevm start --protocol fabric \
    --enable-test-rpc \
    --test-accounts-path testdata/test_accounts.json
```

**Verification**: Test that RPC methods are available:

```bash
# Test eth_accounts
curl -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_accounts","params":[],"id":1}' \
  http://127.0.0.1:8545

# Should return: {"jsonrpc":"2.0","id":1,"result":["0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",...]}
```

### Tests fail with "missing trie node"

This usually indicates state synchronization issues. Ensure:
- Fabric network is fully started before running tests
- Gateway has synced with the latest block
- No concurrent transactions are interfering

### General timeout issues

Increase the timeout in `hardhat.config.js`:

```javascript
networks: {
  fabricevm: {
    url: 'http://127.0.0.1:8545',
    chainId: 4011,
    timeout: 120000, // Increase from 60000
  }
}
```

### Snapshot/revert tests fail

This is expected with the current stub implementation. Tests that rely on state rollback will fail until full snapshot/revert is implemented.

### Gateway fails to start

Check the gateway logs for errors:

```bash
# If using run_hardhat_test.sh, logs are in /tmp/gateway_*.log
tail -f /tmp/gateway_*.log
```

Common issues:
- **Port 8545 already in use**: Kill existing processes on port 8545
- **Missing test accounts file**: Ensure `testdata/test_accounts.json` exists
- **Fabric network not running**: Start with `make start-3` first

## Debugging Guide

### Diagnostic Commands

```bash
# Check if gateway is running
ps aux | grep fxevm

# Check if port 8545 is listening
lsof -i :8545

# Test eth_accounts manually
curl -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_accounts","params":[],"id":1}' \
  http://127.0.0.1:8545 | jq

# Test eth_chainId
curl -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
  http://127.0.0.1:8545 | jq

# Check gateway logs
tail -f /tmp/gateway_*.log

# Check Fabric chaincode containers
docker ps | grep ccaas
```

### Common Issues and Solutions

#### Issue: Tests timeout during "before each" hook

**Symptom**: `Error: Timeout of 40000ms exceeded`

**Cause**: Gateway not started with test RPC enabled

**Solution**: Ensure gateway started with `--enable-test-rpc` flag:
```bash
go run ./cmd/fxevm start --protocol fabric \
    --enable-test-rpc \
    --test-accounts-path testdata/test_accounts.json
```

#### Issue: Transaction processing hangs

**Cause**: WorkerCount set to 0 in configuration

**Solution**: Set `WorkerCount: 4` in `integration/config.go`

#### Issue: Invalid opcode errors (MCOPY, TSTORE, TLOAD)

**Cause**: EVM version not set to Cancun

**Solution**: Enable Cancun in `common/chain.go`:
```go
zero := uint64(0)
return &params.ChainConfig{
    ShanghaiTime: &zero,
    CancunTime:   &zero,  // Enable Cancun opcodes
}
```

#### Issue: Tests expecting reverts timeout

**Symptom**: Tests with `.to.be.revertedWith*` timeout after 40 seconds

**Cause**: Revert detection not properly implemented in JSON-RPC responses

**Status**: Known issue - 50/108 tests affected. Core functionality works (58 tests passing).

**Next Steps**: Investigate `eth_getTransactionReceipt` status field handling

## Contributing

To extend Hardhat compatibility:

1. Identify missing RPC methods from test failures
2. Add method stubs to `gateway/testimpl/hardhat_helpers.go`
3. Register methods in `gateway/testimpl/server.go`
4. Implement full functionality if needed for test success
5. Update this documentation

## References

- [Hardhat Network Reference](https://hardhat.org/hardhat-network/docs/reference)
- [OpenZeppelin Contracts](https://github.com/OpenZeppelin/openzeppelin-contracts)
- [Ethereum JSON-RPC Specification](https://ethereum.github.io/execution-apis/api-documentation/)
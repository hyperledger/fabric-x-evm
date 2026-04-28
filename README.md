<!--
SPDX-License-Identifier: Apache-2.0 AND LGPL-3.0-or-later
-->

# fabric-x-evm

fabric-x-evm makes Hyperledger Fabric-x (and Fabric) compatible with the
Ethereum ecosystem by providing an Ethereum-style JSON-RPC API and native
support for EVM smart contracts within Fabric's permissioned environment. This
integration combines the rich Ethereum tooling and contract ecosystem with
Fabric's robust endorsement and consensus model.

By embedding an Ethereum Virtual Machine (EVM) inside Fabric, developers can
deploy and execute existing Ethereum smart contracts without modification,
while benefiting from Fabric's enterprise-grade features including fine-grained
access control, privacy, and deterministic consensus. The solution preserves
Fabric's transaction flow, trust guarantees, and high performance while
enabling seamless use of the Ethereum development toolchain—including Solidity,
Hardhat, Foundry, and MetaMask.

This approach broadens Fabric's appeal and lowers the barrier for organizations
that want to leverage existing Ethereum assets, developer expertise, and
tooling in a permissioned, enterprise blockchain setting.

## Design

[Here](docs/ARCHITECTURE.md)

## Unit tests

```shell
make unit-tests
```

## Integration tests

### Local

The simplest integration tests don't require a Fabric network, but still
exercise the basic functionality of creating read/write sets out of EVM
transactions, and subsequently reading them.

```shell
make test-local
```

### Fabric-X

Generate the crypto material once:

```shell
make init-x
```

Then start the Fabric-X testcontainer and create the namespace, run the
integration tests against it, and stop it again:

```shell
make start-x
make test-x
make stop-x
```

The container does not keep state.

### Fablo

Start the network, run the integration tests, and stop it again:

```shell
make start-fablo
make test-fablo
make stop-fablo
```

## Interacting with the system

Follow this guide to deploy a smart contract and interact with it.

### Prerequisites

- [solc](https://docs.soliditylang.org/en/latest/installing-solidity.html) - Solidity compiler (`brew install solidity`)
- [cast](https://book.getfoundry.sh/getting-started/installation) - Foundry CLI
- [Metamask](https://metamask.io/) (optional) - browser wallet extension

### Preparation

#### Install dependencies and compile the ERC-20 smart contract

The smart contracts are already provided in the `solidity/` directory. Install
the OpenZeppelin dependencies and compile:

```shell
cd solidity/OzepERC20
npm install
cd ../..

solc --bin --abi --storage-layout --overwrite --evm-version paris \
    -o bin/GLDToken \
    --base-path solidity/OzepERC20 \
    --include-path solidity/OzepERC20/node_modules \
    solidity/OzepERC20/GLDToken.sol
```

#### Generate a wallet

Copy the address and private key, they will be the deployer / admin wallet for
the contract.

```shell
cast wallet new
```

#### Configure Metamask

Follow instructions
[here](https://support.metamask.io/configure/networks/how-to-add-a-custom-network-rpc)
to add a custom network with RPC URL http://localhost:8545 and chain id 4011.

> [!IMPORTANT]
> If you have used the wallet before and start with a clean network, reset the
> nonce of the wallet by going to Settings -> Advanced -> Clear activity tab
> data.

#### Start Fabric

```shell
make start-fablo
```

### Running the application

Start the application:

```shell
cd gateway
go run .
```

#### Export environment variables

In another terminal, export the addresses and keys.

```shell
export ADMIN_ADDRESS=0xabc
export PRIVATE_KEY=0xdef

export METAMASK=0x123
```

Deploy the smart contract and remember the address:

```shell
export CONTRACT_ADDRESS=$(cast send --rpc-url http://localhost:8545 --chain-id 4011 --private-key $PRIVATE_KEY \
  --create "$(cat bin/GLDToken/GLDToken.bin)$(cast abi-encode 'constructor(uint256)' 1000000000000000000000 | sed 's/^0x//')" \
  | awk '/contractAddress/ {print $2}')
```

Transfer 100 tokens to the metamask wallet:

```shell
cast send --rpc-url http://localhost:8545 --chain-id 4011 --private-key $PRIVATE_KEY $CONTRACT_ADDRESS "transfer(address,uint256)" $METAMASK "100000000000000000000"
```

Go to your browser and verify that you can see the tokens. Copy the address of
the admin and send some Gold back.


## License

This repository uses different licenses for different components:

- **Go code**: All Go source code in this repository is released under **LGPL-3.0-or-later** (see `LICENSE.LGPL3`)
- **Scripts**: All scripts are released under **Apache-2.0** (see `LICENSE.Apache2`)

## SPDX License Expression

```
SPDX-License-Identifier: Apache-2.0 AND LGPL-3.0-or-later
```
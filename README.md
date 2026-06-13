<!--
SPDX-License-Identifier: Apache-2.0 AND LGPL-3.0-or-later
-->

# fabric-x-evm

**Run Ethereum smart contracts on Hyperledger Fabric-X.**

fabric-x-evm adds an Ethereum-style JSON-RPC API and a native EVM to Fabric, so
you can deploy unmodified Solidity contracts and use the tooling you already
know - Hardhat, Foundry, MetaMask - against a permissioned, enterprise
blockchain.

By embedding the EVM inside Fabric you get the Ethereum contract ecosystem and
developer experience while keeping Fabric's enterprise strengths: fine-grained
access control, privacy, deterministic consensus, and high performance. Existing
Ethereum assets, skills, and tools carry over with no rewrite — lowering the
barrier for organizations that want Ethereum compatibility in a permissioned
setting.

## Highlights

- **Drop-in EVM** - deploy existing Solidity contracts without modification.
- **Standard JSON-RPC** - point any Ethereum client at `:8545` (chain ID `4011` / `0xfab`).
- **Familiar tooling** - Hardhat, Foundry, MetaMask, and block explorers just work.
- **Fabric trust model** - endorsement, consensus, and access control preserved.
- **Fabric and Fabric-X** - deploy into any existing or new network to add EVM capabilities.

## Try it out

The fastest way to see Fabric-X EVM in action is in the samples repository. It includes a full
network, a block explorer, and a token deploy-and-transfer demo. No need to clone or build this repo.

👉 **[hyperledger/fabric-x-samples → evm](https://github.com/hyperledger/fabric-x-samples/tree/main/evm)**

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — how the gateway, EVM, and Fabric fit together
- [Compatibility](docs/COMPATIBILITY.md) — which Ethereum/EVM guarantees hold, and the caveats
- [JSON-RPC errors](docs/JSON_RPC_ERRORS.md) — error codes the gateway returns

## Building and running from source

This section is for developing against locally built code. If you just want to
use the chain, the [samples repo](https://github.com/hyperledger/fabric-x-samples/tree/main/evm)
above is the easier path.

You'll need [Go](https://go.dev/dl/) and Docker (or Podman) for the Fabric-X network.

Build the `fxevm` binary:

```shell
make build      # produces bin/fxevm
```

Bring up a local Fabric-X network (committer + application namespace):

```shell
make init-x     # generate crypto material (one-time)
make start-x    # start the Fabric-X test network
```

Then run the gateway from your local build, pointed at that network. The sample
config uses paths relative to `integration/`, so run it from there:

```shell
cd integration && ../bin/fxevm start -c fabx.yaml
```

The gateway now serves Ethereum JSON-RPC at **http://localhost:8545**
(chain ID `4011`) — point any Ethereum tooling at it. Stop the network with:

```shell
make stop-x
```

> [!NOTE]
> **Rootless Podman**: pass `DOCKER=podman COMPOSE="podman compose"` to any
> `make` target that starts or stops containers.

## Configuration

The gateway is configured via a YAML file passed to the `start` command with `-c`:

```shell
fxevm -c path/to/config.yaml start
```

See [`integration/fabx.yaml`](integration/fabx.yaml) for a complete annotated
example. The top-level sections are:

| Section     | Description                                                         |
| ----------- | ------------------------------------------------------------------- |
| `logging`   | Log format and level spec                                           |
| `network`   | Channel, namespace, chain ID, and protocol (`fabric` or `fabric-x`) |
| `gateway`   | Listen address, identity, database, orderers, committer             |
| `endorsers` | One entry per embedded endorser peer                                |

### Environment variable overrides

Any config field can be overridden at runtime without editing the file. The
variable name is `GATEWAY_<SECTION>_<FIELD>`, uppercased with dots and hyphens
replaced by underscores. For example:

```shell
GATEWAY_LOGGING_SPEC=debug fxevm -c config.yaml start
GATEWAY_NETWORK_CHANNEL=mychannel fxevm -c config.yaml start
GATEWAY_GATEWAY_LISTEN=0.0.0.0:9545 fxevm -c config.yaml start
```

## Testing

### Unit tests

```shell
make unit-tests
```

### Integration tests

Some integration tests rely on the `ethereum/tests` corpus, vendored as a git
submodule under `testdata/ethereum-tests`. Initialize it once before running
those tests:

```shell
git submodule update --init --recursive
```

#### Local

The simplest integration tests don't require a Fabric network, but still
exercise the basic functionality of creating read/write sets out of EVM
transactions, and subsequently reading them.

```shell
make test-local
```

#### Fabric-X

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

#### Fablo

Start the network, run the integration tests, and stop it again:

```shell
make start-fablo
make test-fablo
make stop-fablo
```

## Build your own contracts

Because the gateway speaks standard Ethereum JSON-RPC, **any Solidity tutorial
works unchanged** — just point the tool's network at the gateway instead of a
public testnet:

| Setting  | Value                            |
| -------- | -------------------------------- |
| RPC URL  | `http://localhost:8545`          |
| Chain ID | `4011`                           |
| Gas      | free — no account funding needed |
| Accounts | any key works                    |

Good starting points, pointed at the URL above:

- **[Foundry](https://book.getfoundry.sh/)** — deploy with `forge create --rpc-url http://localhost:8545 ...` or send transactions with `cast send --rpc-url http://localhost:8545 ...`.
- **[Hardhat](https://hardhat.org/tutorial)** — add a network entry in `hardhat.config.js` with `url: "http://localhost:8545"` and `chainId: 4011`, then deploy as usual.
- **[MetaMask](https://support.metamask.io/configure/networks/how-to-add-a-custom-network-rpc/)** — add a custom network with the RPC URL and chain ID to interact from the browser.

A few things differ from a public chain — see [Compatibility](docs/COMPATIBILITY.md)
for the details (gas/fee fields are stubbed, access control is Fabric's, etc.).
For a complete worked example, see the
[samples repo](https://github.com/hyperledger/fabric-x-samples/tree/main/evm).

## License

This repository uses different licenses for different components:

- **Go code**: All Go source code in this repository is released under **LGPL-3.0-or-later** (see `LICENSE.LGPL3`)
- **Scripts**: All scripts are released under **Apache-2.0** (see `LICENSE.Apache2`)

### SPDX License Expression

```
SPDX-License-Identifier: Apache-2.0 AND LGPL-3.0-or-later
```

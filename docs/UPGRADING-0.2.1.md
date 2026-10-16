# Upgrading from 0.1.3 to 0.2.1

This covers what changes for deployments that run the
`ghcr.io/hyperledger/fabric-x-evm` image: the config file, the processes and
ports, and the data directory. The API additions are listed at the end.

**At a glance**

- A 0.1.3 config file does not start on 0.2.1. The committer connection moves
  to the top level, and unknown keys are rejected at startup.
- On Fabric-X, `network.ns-version` is now the namespace's MVCC version
  counter. The `"1.0"` used in 0.1.3 examples means version 1, and it has to be
  `"0"` for a namespace that has never been updated.
- The JSON-RPC server now checks the `Host` header, and only allows `localhost`
  unless you configure more. It also no longer sends CORS headers.
- A second process type is new: a **standalone endorser**. It uses the same
  image and the same `start` command, and lets another org take part in
  endorsement. An existing single-org deployment does not need it.
- The SQLite files keep their schema. How safe it is to keep them depends on
  which file is further ahead (see [Data directory](#data-directory)).

## Config file

### Required changes for an existing single-org deployment

A gateway with its embedded endorser (the only topology 0.1.3 supported in
production) needs four changes:

1. **One `committer:` block, at the top level.** Delete `gateway.committer` and
   `endorser.committer` and put a single `committer:` at the root. Its shape
   (`endpoint`, `tls`) is the same as before. 0.1.3 opened two block-delivery
   streams to the sidecar, one per component. 0.2.1 opens one per process and
   feeds both the gateway and the embedded endorser from it.
2. **`gateway.sync-timeout` is now `synchronizer.timeout`**, at the top level.
   The default stays 10 minutes if you leave it out.
3. **`network.ns-version` is the namespace version (Fabric-X only).** See
   [Namespace version](#namespace-version).
4. **`gateway.vhosts`**, if clients reach the gateway by hostname (see
   [JSON-RPC Host check and CORS](#json-rpc-host-check-and-cors)).

Everything else under `network:` (apart from the meaning of `ns-version`), `logging:`, `gateway:` (listen, identity,
database, orderers, worker/submitter counts) and `endorser:` (name, identity,
database) keeps its name and meaning.

Unknown keys make the process fail at startup. A leftover `gateway.committer`
or `endorser.committer` is reported as an error, not ignored. The same applies
to `endorser.debug-logs`, which has been removed.

```yaml
# 0.1.3                                # 0.2.1
network: {...}                         network: {...}     # ns-version: "0" on Fabric-X
logging: {...}                         logging: {...}
                                       committer:          # moved here, once
                                         endpoint: {host: ..., port: ...}
                                         tls: {...}
                                       synchronizer:       # optional
                                         timeout: 10m
                                         all-tx-queue-depth: ...  # optional, Fabric-X
gateway:                               gateway:
  listen: ...                            listen: ...
  identity: {...}                        identity: {...}
  database: {...}                        database: {...}
  orderers: [...]                        orderers: [...]
  committer: {...}      # removed        vhosts: [localhost, ...]   # new, optional
  sync-timeout: ...     # moved          worker-count: ...
  worker-count: ...                      ...
  ...                                    endorsers: [...]   # new meaning, optional
endorser:                              endorser:
  name: ...                              name: ...
  identity: {...}                        identity: {...}
  committer: {...}      # removed        database: {...}
  database: {...}                        server: {...}      # new, optional
  debug-logs: ...       # removed
```

### Namespace version

On `protocol: fabric-x`, `network.ns-version` is now sent with every
transaction as the namespace's MVCC version, and the committer rejects
transactions whose version does not match the namespace's current one. A
namespace starts at version `0`, and its version goes up by one each time the
namespace is updated (for example to change its endorsement policy).

- The value must be a non-negative integer. A zero decimal part is accepted,
  so `"0"`, `"1"` and `"2.0"` are valid and `"1.5"` is a startup error.
- Earlier releases ignored this value and always sent version `0`. `"1.0"`,
  which 0.1.3 configs and samples used, now means version 1. For a
  namespace that was created and never updated, every transaction is then
  rejected. Set it to `"0"`.
- An empty value means `0`.
- On `protocol: fabric` nothing changes: it is still the chaincode version
  label, and any non-empty string is accepted.

### Top-level rules

| Sections present | Process that runs |
|---|---|
| `gateway:` and `endorser:` | Gateway with its embedded endorser (what 0.1.3 ran) |
| `endorser:` only (with `endorser.server`) | Standalone endorser: no JSON-RPC, gRPC only |
| `gateway:` without `endorser:` | Rejected |

`committer:` and `network:` are required in both cases.

### `gateway.endorsers`: new meaning

In 0.1.3, `gateway.endorsers` was an alternative to the embedded endorser: you
set one or the other, never both. In 0.2.1 the embedded endorser is always
present, and `gateway.endorsers` lists **other orgs'** endorsers, which the
gateway dials over gRPC *in addition*. Each entry is a client config
(`endpoint`, `tls`), the same shape as an orderer entry.

### `endorser.server`: new

`endorser.server` is the gRPC listener that other orgs' gateways dial:

```yaml
endorser:
  server:
    endpoint:
      host: 0.0.0.0
      port: 7051
    tls:
      mode: mtls            # none | tls | mtls
      cert-path: /path/server.crt
      key-path: /path/server.key
      ca-cert-paths:        # with mtls: TLS CAs of the orgs whose gateways may dial in
        - /path/org1-tls-ca.pem
    # optional: keep-alive, rate-limit, max-concurrent-streams
```

- It is **required** for a standalone endorser.
- It is **optional** for a gateway's embedded endorser. Set it only if another
  org's gateway needs to reach your endorser.
- `endpoint` must be the `{host, port}` mapping. The `":7051"` string form
  seen in some committer sample configs does not decode here.
- The TLS files are checked at startup, and a missing file is a startup error.

## JSON-RPC Host check and CORS

In 0.1.3 the JSON-RPC server accepted any `Host` header and answered every
origin with `Access-Control-Allow-Origin: *`. In 0.2.1:

- **Host allowlist (`gateway.vhosts`).** The default is `["localhost"]`. A
  request whose `Host` header is an IP address is always accepted. A request
  whose `Host` header is any other name gets `403 invalid host specified`. This
  affects anything that reaches the gateway by name: health checks or smoke
  tests that call the RPC by inventory hostname, Kubernetes Service DNS names,
  ingress hostnames, and Blockscout or other indexers using a service alias.
  Add each of those names. `"*"` turns the check off, and the gateway logs a
  warning at startup when it is set.
- **CORS is off.** No CORS headers are sent, and there is no setting to turn
  them back on. Browser pages served from another origin can no longer call the
  JSON-RPC endpoint directly. Wallet extensions such as MetaMask are not
  affected.
- The allowlist applies to plain HTTP requests. WebSocket connections (see
  below) use their own check: an `Origin` header, if the client sends one, must
  be `http://localhost`, and clients that send no `Origin` are accepted.

## Processes, ports and probes

### Gateway (single-org, as before)

- **Same port, now also WebSocket.** The HTTP listener (`gateway.listen`) also
  accepts WebSocket upgrades for JSON-RPC. There is no new port. A proxy or
  ingress in front of the gateway has to pass `Upgrade`/`Connection` headers
  through for WebSocket clients to work.
- **Unchanged:** the HTTP listener opens only after the initial sync with the
  committer finishes. A TCP probe on it still means "synced and serving", and
  the image's built-in `HEALTHCHECK` (`fxevm healthcheck`, default
  `localhost:8545`) still fits.
- If `endorser.server` is set, the gRPC listener also opens only after the
  initial sync.

### Standalone endorser (new)

This is a new process type for multi-org endorsement. It uses the same image
and the same command line (`start --config <file>`). The config has `network`,
`committer`, and `endorser` with `server`, and no `gateway:` section.

- **One listener: gRPC on `endorser.server.endpoint`.** It has no HTTP or
  JSON-RPC.
- **The image's default healthcheck does not fit.** The Dockerfile's
  `HEALTHCHECK` probes `localhost:8545`, which a standalone endorser never
  opens. Use `fxevm healthcheck --addr localhost:<grpc port>` instead, or a TCP
  probe on the gRPC port. It is a TCP connect only, like the gateway's.
- **The gRPC listener opens only after the initial sync**, so the probe has
  the same meaning as for a gateway.
- **It needs its own committer connection** (top-level `committer:`). It
  replays committed blocks into its own world state, so it should point at a
  committer sidecar that its org trusts. In the samples it borrows Org1's
  sidecar, but that is a shortcut for the demo.
- **It needs its own persistent data directory** for `endorser.database`.
- The image runs as `65534:65534` and declares `EXPOSE 8545` only. The gRPC
  port is not declared in the image.

### Multi-org topology

```
            JSON-RPC                     gRPC (mTLS)
clients ──────────────▶ Org1 process ─────────────────▶ Org2 standalone endorser
                        gateway + embedded                      │
                        Org1 endorser                           │
                           │      │                             │
               orderers ◀──┘      └──▶ committer ◀──────────────┘
                                       (block delivery, one stream per process)
```

The following behaviour is useful to know when you design the topology:

- **All configured endorsers must sign (N-of-N).** Every transaction is sent to
  the embedded endorser and to every entry in `gateway.endorsers`. If any of
  them fails or cannot be reached, that transaction fails. Each remote entry
  therefore adds that org's endorser to the transaction path.
- **The namespace endorsement policy has to match.** The endorsement policy
  set when the namespace is created (for example
  `AND('Org1MSP.member','Org2MSP.member')`) is checked against the
  `endorser.identity` of each process. The gateway does not read the policy. It
  collects exactly the signatures of the endorsers it is configured with.
- **Reads are served locally only.** `eth_call`, `eth_estimateGas`,
  `eth_getBalance`, `eth_getCode`, `eth_getStorageAt` and nonce lookups use the
  gateway's embedded endorser. Remote endorsers only take part in endorsing
  transactions.
- **Remote endorsers are dialed lazily.** A gateway starts even if a remote
  endorser is down. Transactions fail until that endorser is reachable. Missing
  or unreadable TLS files for a `gateway.endorsers` entry are a startup error.
- **TLS material for each gateway→endorser link:**
  - On the endorser (`endorser.server.tls`): a server cert and key. The cert
    must be valid for the hostname the gateway dials, because the client
    verifies the server name against `gateway.endorsers[].endpoint.host`. With
    `mtls`, it also needs the TLS CA certs of every org whose gateway dials in.
  - On the gateway (`gateway.endorsers[].tls`): the remote org's TLS CA cert.
    With `mtls`, it also needs a client cert and key issued by a CA that the
    endorser trusts.
- **Only one gateway per org is tested.** Every org runs an endorser, and only
  orgs that serve JSON-RPC need a gateway. Two orgs can each run a gateway
  (each listing the other in `gateway.endorsers` and both setting
  `endorser.server`), but that setup has not been tested.

## Data directory

- **SQLite schemas are unchanged** for both the gateway database
  (`gateway.database`) and the SQLite endorser backend (`endorser.database`
  with `database: sqlite`).
- **Resume point.** In 0.1.3 the gateway and the embedded endorser synced
  independently, so their two files can be at different block heights when you
  stop 0.1.3. 0.2.1 has one synchronizer per process, and it resumes from the
  **gateway's** height:
  - If the endorser's file is at the same height or further ahead, it is fine.
    Replaying a block into the endorser store is now a no-op.
  - If the endorser's file is **behind** the gateway's, the endorser silently
    misses the blocks in between, and 0.2.1 does not check for this. Its world
    state will then disagree with the chain, and its endorsements will fail
    MVCC validation for the keys involved.
  - Starting from an empty data directory avoids the question. Both stores then
    resync from the committer. On a long chain this can exceed the default
    10-minute `synchronizer.timeout`.
- **PebbleKVS (`database: pebble`) directories from 0.1.3 are not
  compatible.** The on-disk layout changed. It now keeps current values plus a
  bounded history window, so its disk use no longer grows without limit. Start
  pebble endorsers from an empty directory. PebbleKVS is also now allowed with
  `network.protocol: fabric`.

## API additions (no config needed)

- JSON-RPC over WebSocket, on the existing port.
- `eth_subscribe` / `eth_unsubscribe` for `newHeads` (WebSocket only).
- Polling filters: `eth_newBlockFilter`, `eth_newFilter`,
  `eth_getFilterChanges`, `eth_getFilterLogs`, `eth_uninstallFilter`.
  `eth_newPendingTransactionFilter` is not supported.
- `eth_getBlockReceipts` (by number or hash).
- Receipts of transactions that did not create a contract now return
  `contractAddress: null`.
- The gateway accepts transactions with a future nonce and holds them until
  the missing nonces arrive. Before, such transactions were rejected.

## Already on 0.2.0

Coming from 0.2.0 needs only two config changes:

- On Fabric-X, set `network.ns-version` to the namespace's actual version,
  which is usually `"0"` (see [Namespace version](#namespace-version)). 0.2.0
  always sent version `0`, whatever the config said, so a `"1.0"` that worked
  on 0.2.0 now makes every transaction fail.
- Remove `endorser.debug-logs` if it is set.

`synchronizer.all-tx-queue-depth` is new and optional.

## Reference configs

- [`integration/fabx.yaml`](https://github.com/hyperledger/fabric-x-evm/blob/v0.2.1/integration/fabx.yaml):
  a single-org gateway with its embedded endorser.
- [`integration/fabx-2of2.yaml`](https://github.com/hyperledger/fabric-x-evm/blob/v0.2.1/integration/fabx-2of2.yaml)
  and [`integration/fabx-2of2-org2.yaml`](https://github.com/hyperledger/fabric-x-evm/blob/v0.2.1/integration/fabx-2of2-org2.yaml):
  an Org1 gateway that dials an Org2 standalone endorser.
- [`fabric-x-samples/evm`](https://github.com/hyperledger/fabric-x-samples/tree/main/evm)
  ([`gateway-conf.yaml`](https://github.com/hyperledger/fabric-x-samples/blob/main/evm/gateway-conf.yaml),
  [`endorser-org2-conf.yaml`](https://github.com/hyperledger/fabric-x-samples/blob/main/evm/endorser-org2-conf.yaml),
  [`compose.gateway.yaml`](https://github.com/hyperledger/fabric-x-samples/blob/main/evm/compose.gateway.yaml)):
  the same 2-org topology in Docker Compose, with mounts and healthchecks.

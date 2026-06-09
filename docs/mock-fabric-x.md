# Mock Fabric-X for Performance Tests

`mock-fabric-x` is a local in-memory Fabric-X orderer and committer-sidecar replacement for `fabric-x-evm` performance testing.

Use it when you want the existing gateway loadgen/tests to run against a mock Fabric-X backend instead of the real Fabric-X orderer and committer stack.

## What it replaces

`mock-fabric-x` listens on the same endpoints used by `integration/fabx.yaml`:

- Fabric `orderer.AtomicBroadcast.Broadcast`: `127.0.0.1:7050`
- Fabric-X committer replay path on `127.0.0.1:4001`:
  - `BlockQueryService.GetBlockchainInfo`
  - Fabric peer `Deliver`
  - `Notifier.StreamAllTransactions`

This mock is intentionally narrow. It targets the replay/perf harness and does not implement Fabric-X query RPCs such as `GetBlockByNumber`, `GetBlockByTxID`, `GetTxByID`, or `GetTransactionStatus`.

When using this mock, do **not** run `make start-x`. The mock owns the Fabric-X ports that the gateway loadgen already targets.

## Non-goals

This mock does not implement consensus, MVCC validation, persistence, namespace policy validation, or real committer database behavior. Use metrics from this backend as mock-backend/gateway-loadgen capacity only, not real Fabric-X orderer or committer capacity.

## One-time setup

Generate TLS material and config files used by `integration/fabx.yaml`:

```bash
make init-x
```

On Docker Desktop or bind mounts where container UID `501` cannot write to the mounted `testdata` directory, use container root for generation:

```bash
make init-x UID=0 GID=0
```

## Build

```bash
make build-mock-fabric-x
```

## Start mock Fabric-X

Run the mock from the repository root. The default TLS paths are relative to that directory and match the `../testdata/...` paths used when Go runs `integration/fabx.yaml` tests from `integration/`.

```bash
./bin/mock-fabric-x \
  --max-tx-per-block=500 \
  --block-timeout=10ms
```

Leave it running in one terminal. In another terminal, run the gateway/loadgen tests from the repository root.

## Run existing perf replay loadgen

The perf replay test uses `integration/fabx.yaml`, so no test-code changes are needed.

```bash
PERF_REPLAY_WINDOW_SIZE=100 \
PERF_REPLAY_WRAP_COUNT=1 \
PERF_SUBMITTING_WORKERS=16 \
PERF_PROCESSING_WORKERS=8 \
go test -v -count=1 -tags=perf -run '^TestReplayJSONDataset$' -timeout 5m ./integration/perf
```

Expected successful smoke output resembles:

```text
Loaded 100 transfers from dataset
Replay complete: 100 successful, 0 failed, 0 skipped out of 100 total transfers
gw stats: 101 0 0
--- PASS: TestReplayJSONDataset
```

## Dataset prerequisites

`TestReplayJSONDataset` needs local perf data files:

- `integration/perf/testdata/USDC_contract.json`
- `integration/perf/testdata/USDC_dataset.json.gz`

These large/generated files are not committed. To create real USDC replay data, follow:

```text
integration/perf/USDC_testing.md
```

For a quick local smoke, you can generate a small synthetic TSV and then use the existing perf generator. The successful smoke run used a generated 100-transfer dataset, not the full Harvard dataset.

## Insecure local run

Use `--tls-mode none` only with configs that disable TLS.

```bash
./bin/mock-fabric-x --tls-mode none --orderer-listen 127.0.0.1:17050 --committer-listen 127.0.0.1:14001
```

## Useful flags

```text
--orderer-listen      orderer AtomicBroadcast listen address
--committer-listen    committer sidecar listen address
--tls-mode            mtls or none
--max-tx-per-block    maximum transactions per mock block
--block-timeout       maximum time before cutting a partial block
--queue-size          accepted envelope queue size
```

Examples:

```bash
# Larger blocks
./bin/mock-fabric-x --max-tx-per-block=1000 --block-timeout=20ms

# Immediate one-transaction blocks
./bin/mock-fabric-x --max-tx-per-block=1 --block-timeout=0
```

## Troubleshooting

### Port already in use

Stop real Fabric-X before starting the mock:

```bash
make stop-x
```

Then start `mock-fabric-x` again.

### TLS cert path errors

Run `mock-fabric-x` from the repository root. The default orderer router certs live under the `party1` directory:

```text
testdata/crypto/ordererOrganizations/orderer-org-1/orderers/party1/router.orderer-org-1/tls/server.crt
testdata/crypto/ordererOrganizations/orderer-org-1/orderers/party1/router.orderer-org-1/tls/server.key
```

If you run from another directory, either `cd` back to the repository root or pass absolute `--orderer-cert`, `--orderer-key`, `--committer-cert`, `--committer-key`, and client CA paths.

### Missing perf files

If the test fails with missing `USDC_contract.json` or `USDC_dataset.json.gz`, generate them first using `integration/perf/USDC_testing.md`.

### Inspect mock logs

Run mock with log capture:

```bash
./bin/mock-fabric-x ... > /tmp/mock-fabric-x.log 2>&1
```

Then inspect:

```bash
tail -100 /tmp/mock-fabric-x.log
```

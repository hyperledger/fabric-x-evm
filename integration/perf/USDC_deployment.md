# USDC ERC-20 Performance Demo

## Goal

Demonstrate that Fabric-X-EVM can sustain high aggregate TPS of ERC-20 token transfers by replaying historical USDC mainnet transactions across N parallel gateway replicas.

---

## What it tests

The workload replays ~150k historical USDC transfers through the full Fabric-X pipeline:

```
eth_sendRawTransaction
  → Gateway (JSON-RPC, per-replica)
  → Endorser (EVM simulation)
  → Fabric-X Ordering Service  (shared)
  → Committer (MVCC validation) (shared)
  → Delivery → Gateway (state update)
```

**Scaling model:** each replica has its own Gateway + Endorser + Fabric namespace. All replicas share one ordering service and one committer. Delivery is namespace-filtered so each gateway only processes its own namespace's blocks. Without this, throughput would be O(1/N) as N grows.

**Balance priming:** historical senders need a balance. Rather than pre-loading state, balance reads that return zero are transparently intercepted and return a synthetic 1B USDC. The ledger state is not meaningful — this is a throughput demo, not a correctness test.

---

## Current benchmark results

**Post-fix baseline** — 8 submitting workers, 3 replicas:

| Replica | Host | TPS (overall) | Successful |
|---------|------|---------------|------------|
| 1 | dectrust5 | 10.50 | 3000 |
| 2 | dectrust6 | 10.25 | 3000 |
| 3 | dectrust7 | 10.00 | 3000 |
| **Aggregate** | | **~30 tx/s** | **9000** |

All transactions had `receipt.Status=0x1` (EVM success). The bottleneck is worker count vs. per-tx latency (~1s). See throughput analysis below.

**Previous (broken) baseline** — 8 workers, 3 replicas, commit `605e744`:
Reported 3000/3000 successful at ~8.7 tx/s per replica, ~26 tx/s aggregate.
**Actual:** all 3000 had `status=0x0` (EVM revert). The broken success counter (which only checked `pending=false`) hid this.

---

## BalancePrimingWrapper bug — root cause of all EVM reverts

### What broke

`BalancePrimingWrapper.GetState` was priming **every zero storage slot** of the USDC contract address to a large synthetic value (1B × 10¹²). This included:
- `_paused` (slot 1): primed to `1e24` → EVM interprets as `true` → `whenNotPaused` modifier reverts every transfer
- `_owner`, `_blacklister`, etc.: similarly corrupted

The intended behavior was to prime only the **sender's balance slot** (`keccak256(sender, 9)`) to give senders a synthetic 1B USDC balance when their real balance is zero.

### What was wrong

```go
// BEFORE (broken): primed ALL zero slots of contractAddr
func (w *BalancePrimingWrapper) GetState(addr, slot) {
    if w.enabled && addr == w.contractAddr {
        if realValue == zero { return primeValue }  // ← affects _paused, _owner, etc.
    }
}

// SetSender was also broken: didn't compute the balance slot
func (w *BalancePrimingWrapper) SetSender(sender common.Address) {
    w.enabled = true
    // ← senderAddr and balanceSlot never set
}

// BalancePrimingExecutor.Execute passed wrong sender
sa.SetSender(common.Address{})  // ← always empty address, not actual sender
```

### Fix (commit `83ccaf6`)

1. `SetSender(sender)` now computes `w.balanceSlot = GetERC20BalanceSlot(sender, w.mappingPosition)`.
2. `GetState` now only primes when `addr == w.contractAddr && slot == w.balanceSlot`.
3. `BalancePrimingExecutor.Execute` now recovers the actual sender from the signed transaction and passes it to `SetSender`.

### Why it wasn't caught earlier

The success counter only checked `pending=false` (transaction committed to Fabric ledger), not `receipt.Status=0x1` (EVM execution succeeded). All ~8.7 tx/s were committed-but-reverted transactions. The receipt-status check added in this investigation revealed 100% failure rate.

---

---

## Throughput analysis

### Measured latency budget per transaction (~1s total)

From direct inspection of the staging deployment configs and assembler logs:

| Stage | Time | Source |
|---|---|---|
| Batcher queue (avg) | **~250ms** | `BatchTimeout=500ms` in genesis + `BatchCreationTimeout=500ms` in ArMA batcher + `requestbatchmaxinterval=500ms` in SmartBFT |
| BFT consensus | ~5ms | Assembler logs: decisions arrive ~0-5ms apart at steady state |
| Block ledger append (YugabyteDB) | ~3ms | `Appended block N in 2-3ms` in assembler logs |
| Committer pipeline (sidecar → coordinator → 4 verifiers → 4 validators) | ~250–500ms | Estimated; network RTT between dectrust VMs is <1ms so this is coordinator overhead |
| Delivery to gateway synchronizer | ~1ms | Sub-ms network; Recv() is streaming |
| **Total** | **~510–760ms** | |

The expected per-tx latency is ~500ms. We observe ~1s. The gap is explained by the batcher queue: 500ms `BatchTimeout` means an average 250ms queue wait, so the budget is closer to 510–760ms, consistent with the observation. The remaining variance is committer pipeline timing (uncharacterized).

### armaSubmitter submission strategy — CONFIRMED: broadcast-to-all is correct

`gateway/app/arma_submitter.go` `Submit()` broadcasts to all 4 orderers in parallel. This was suspected to cause extra latency by waiting for the slowest.

**Tested:** switching to round-robin (one orderer at a time) dropped throughput to **0.5 tx/s** — 18× worse. The cause: ArMA has `leaderrotation: false`, so the batcher leader is fixed. When round-robin hits a non-leader, that batcher tries to forward to the leader and waits up to `requestforwardtimeout: 10s` before giving up. Broadcasting to all ensures the leader always receives the tx directly; non-leaders detect duplicates quickly once the leader processes it.

**Conclusion:** broadcast-to-all is correct and fast (~2–5ms total for 4 parallel gRPC calls). This is NOT the cause of the 1s latency.

### BatchTimeout is 500ms — a major latency floor

All three independent configs confirm `BatchTimeout=500ms`:
```
genesis block:              BatchTimeout: 500ms
configtxgen template:       BatchTimeout: 500ms
shared_config.yaml.j2:      BatchCreationTimeout: 500ms
                            requestbatchmaxinterval: 500ms (SmartBFT)
```

At this setting, the average batcher queue time is 250ms regardless of worker count or pipeline changes. Reducing to 100ms or 50ms would cut this to 50ms or 25ms.

**Status:** Ansible templates changed (`BatchTimeout: 50ms`, `requestbatchmaxinterval: 50ms`, `BatchCreationTimeout: 50ms`). Network restart blocked: the staging inventory (`examples/inventory/ibmcloud/fabric-x-evm.yaml`) contains placeholder hostnames (`fill_in_orderer_vm_ip`), so Ansible cannot reach dectrust1–4 from outside the VPC. Needs to be run from dectrust8 with a properly configured inventory, or the inventory file needs to be updated. **Pending deployment.**

### Worker count is an immediate multiplier

`TestReplayJSONDataset` defaults to 8 submitting workers. At ~1s per-tx latency, scaling workers is an immediate TPS multiplier:

| Submitting workers | Expected TPS/replica | × 3 replicas |
|---|---|---|
| 8 (baseline) | ~8 | ~26 |
| 24 | ~24 | ~72 |
| 32 | ~32 | ~96 |
| 64 | ~64 | ~192 |

**Status:** Added `PERF_SUBMITTING_WORKERS` env var to `TestReplayJSONDataset`. Extended `TestReplayJSONDatasetPerformance` sweep to `[4, 8, 16, 24, 32, 64]`. Currently running 32-worker test on staging via `make run-demo DEMO_ARGS="--submitting-workers 32"`.

### Success counting — FIXED

`pending=false` only means the tx appears in a committed block. It does NOT mean EVM execution succeeded. A tx with `status=0x0` (EVM revert) was previously counted as success.

**Fixed:** after `pending=false`, the workload now calls `eth_getTransactionReceipt` and asserts `status == 0x1`, panicking if the EVM reverted. With the `NewSnapshot` fix on this branch, all transfers succeed (status=0x1), so the success count is accurate.

### Local TPS is also low

Local TPS is similarly low (~8–10 tx/s), ruling out staging-specific factors (network latency, VM load). This confirms the bottleneck is in the BatchTimeout / worker count, not the staging environment.

**Implication:** the BatchTimeout reduction (Step 2) and worker scaling (Step 3) are the right levers regardless of environment.

---

## Investigation plan (to be executed in order)

### Step 1: armaSubmitter submission strategy — TESTED, broadcast-to-all is correct

**Hypothesis tested:** broadcasting to all 4 orderers and waiting for the slowest causes excess latency.

**Result:** WRONG. Round-robin (submit to one, retry on failure) was tested and produced **0.5 tx/s** — 18× worse. The cause: ArMA has `leaderrotation: false`, so the batcher leader is fixed. When we hit a non-leader batcher with round-robin, it tries to forward the tx to the leader and waits up to `requestforwardtimeout: 10s` before failing. Broadcasting to all ensures the leader always receives the tx directly; non-leaders detect it as a duplicate quickly once the leader processes it.

**Conclusion:** broadcast-to-all is the correct strategy for ArMA with fixed leader rotation. The submission overhead is 4 parallel gRPC calls (fast, ~2–5ms total), not a bottleneck.

### Step 2: Reduce BatchTimeout from 500ms to 50ms — BLOCKED

**Hypothesis:** 500ms batch cut interval contributes a fixed ~250ms average queuing delay; reducing it would directly improve per-tx latency and allow lower worker counts to achieve target TPS.

**Expected result:** latency drops from ~1s to ~550ms (25ms avg queue + ~500ms committer) → same 8 workers → ~13–15 tx/s per replica.

**Action taken:**
1. Changed `BatchTimeout: 500ms → 50ms` in `configtxgen/templates/configtx.yaml.j2`
2. Changed `requestbatchmaxinterval: 500ms → 50ms` and `BatchCreationTimeout: 500ms → 50ms` in `armageddon/templates/shared_config.yaml.j2`
3. Pushed to fork (`kushnireyal/fabric-x-ansible-collection` `evm-gateway-demo`, commit `cbb2126`).

**Blocked:** network restart requires regenerating the genesis block (BatchTimeout is baked into it). The staging Ansible inventory (`examples/inventory/ibmcloud/fabric-x-evm.yaml`) has placeholder hostnames — Ansible cannot reach dectrust1–4 from outside the VPC. Must be run from dectrust8 with a fully configured inventory.

**To unblock:** either (a) update the inventory on dectrust8 with real internal IPs and run `make fabric_x start` from dectrust8, or (b) manually redeploy the Fabric-X network with a fresh genesis block.

### Step 3: Scale worker count — IN PROGRESS

**Action:**
1. Added `PERF_SUBMITTING_WORKERS` env var to `TestReplayJSONDataset` — worker count is now configurable without code changes.
2. Extended `TestReplayJSONDatasetPerformance` sweep to `[4, 8, 16, 24, 32, 64]`.
3. Currently running: `make run-demo DEMO_ARGS="--skip-testdata --quiet --submitting-workers 32"`.
4. Next: run `TestReplayJSONDatasetPerformance` for the full automated sweep and plot TPS vs workers.

**Expected:** roughly linear scaling up to where the ordering service or committer saturates. At 32 workers with ~1s latency, expect ~32 tx/s per replica (~96 aggregate).

**Files:** `integration/perf/replay_json_dataset_test.go`

### Step 4: Add receipt-status verification to the success counter — DONE (revealed critical bug)

After `pending=false`, the workload now calls `eth_getTransactionReceipt` and panics if `status != 0x1`. This check revealed that **all prior transfers were EVM-reverting** (100% failure rate at status=0x0), which was hidden by the broken pending=false counter. The receipt check directly led to diagnosing and fixing the `BalancePrimingWrapper` bug.

**Files:** `integration/perf/replay_json_dataset_test.go`

### Step 5: Measure committer pipeline latency in isolation — DONE

**Action taken:**
1. Added `submitTimes map[common.Hash]time.Time` to `TxQueueV2` and `RecordSubmit(hash)` method.
2. `processTx` in `gateway/core/api.go` calls `RecordSubmit` after `SubmitFabricTx` returns.
3. `TxQueueV2.completeUnlocked` logs `tx <hash>: submit→commit=<N>ms` at INFO level via `gateway.core.txqueue_v2` logger.

**To read the output:** enable the logger with `flogging.ActivateSpec("gateway.core.txqueue_v2=info")` or set `FABRIC_LOGGING_SPEC=gateway.core.txqueue_v2=info` in the environment. Each committed tx will log its submit→commit latency.

**Next:** run the workload on staging and grep for `submit→commit` lines to see the distribution. If values cluster around 500–750ms, BatchTimeout reduction (Step 2) is the right lever. If values cluster > 1s, investigate the committer pipeline config (`dependency-graph.num-of-local-dep-constructors`, verifier/validator parallelism).

**Files:** `gateway/core/txqueue_v2.go`, `gateway/core/api.go`

### Step 6: Test against all-in-one container baseline

**Action:**
1. Start the all-in-one Fabric-X container locally: `docker run ... fabricx-all-in-one:latest`.
2. Point `integration/fabx.yaml` at it and run `TestReplayJSONDataset`.
3. Compare TPS and per-tx latency; if 60 tx/s reproduced, the bottleneck is confirmed to be in our gateway/submitter path, not the Fabric-X network.

**Note:** the user confirmed local TPS is also low (~8–10 tx/s); this step establishes whether the Fabric-X backend itself is fast and only our submission path is slow.

---

## Repos and branches

| Repo | Branch | Purpose |
|---|---|---|
| `github.com/hyperledger/fabric-x-evm` | `evm-gateway-demo` | EVM gateway + workload test |
| `github.com/kushnireyal/fabric-x-ansible-collection` | `evm-gateway-demo` | Deployment scripts + Ansible role |

---

## Running locally (single replica)

The scripts work on both macOS and Linux. The only difference is `LOCAL_ANSIBLE_HOST`:
- **macOS** (Docker Desktop): use `host.docker.internal`
- **Linux** (Docker Engine): use `localhost`

**One-time setup:**

```bash
# Clone the EVM repo
git clone https://github.com/hyperledger/fabric-x-evm.git

# Clone the Ansible collection fork
git clone https://github.com/kushnireyal/fabric-x-ansible-collection.git \
  ~/.ansible/collections/ansible_collections/hyperledger/fabricx
cd ~/.ansible/collections/ansible_collections/hyperledger/fabricx
git checkout evm-gateway-demo && make install-deps

# macOS only — makes host.docker.internal resolve on the host side
grep -q "host.docker.internal" /etc/hosts || \
  echo "127.0.0.1 host.docker.internal" | sudo tee -a /etc/hosts
```

**Run:**

```bash
cd ~/.ansible/collections/ansible_collections/hyperledger/fabricx

LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm make demo-local DEMO_ARGS=--quiet  # macOS
LOCAL_ANSIBLE_HOST=localhost            EVM_REPO=/path/to/fabric-x-evm make demo-local DEMO_ARGS=--quiet  # Linux
```

**Fast iteration — `--warm` mode** (skip Fabric-X network setup, ~2 min saved):

```bash
# First run: full setup, leave stack running
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  make demo-local DEMO_ARGS="--skip-teardown --quiet"

# All subsequent runs: reuse the stack, only restart gateway if code changed
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  make demo-local DEMO_ARGS="--warm --quiet"
```

**Increasing transaction count for stability measurement:**

```bash
# Replay 3000-tx window 10× = 30 000 transactions
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  PERF_REPLAY_WRAP_COUNT=10 make demo-local DEMO_ARGS="--warm --quiet"
```

---

## Running on staging (3 replicas)

Staging topology: `dectrust1–4` run the Fabric-X network; `dectrust5–7` run EVM gateways; `dectrust8` is the control node. All machines: 32 vCPUs, 64 GB RAM.

**From your Mac:**

```bash
cd /path/to/fabric-x-evm

make run-demo                                           # current branch, full run
make run-demo DEMO_ARGS="--skip-testdata --quiet"       # testdata already present
make run-demo EVM_BRANCH=my-feature DEMO_ARGS=--quiet  # specific branch
```

`make run-demo` defaults `EVM_BRANCH` to the current local branch. It SSHes to `dectrust8`, syncs the Ansible collection, checks out the EVM branch on each staging VM, builds a Docker image tagged `fabric-x-evm:<sha>` (skipped if already built), then runs the workload.

**From dectrust8 directly:**

```bash
make deploy-staging EVM_BRANCH=my-feature
make demo-staging   EVM_BRANCH=my-feature DEMO_ARGS=--skip-deploy
```

---

## Testdata (staging)

Three files are not in the git repo and must be present on dectrust5–7. `run-demo.sh` generates and distributes them automatically unless `--skip-testdata` is passed.

To generate manually on dectrust5, then copy to dectrust6/7:

```bash
ssh dectrust5.vpc.cloud9.ibm.com

# Download Ethereum token transfer dataset (~10 GB, one-time)
cd /data
wget https://dataverse.harvard.edu/api/access/datafile/11691882 -O 202001.tsv.gz

# Filter to USDC-only transfers
zgrep -E '(^.{93}a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48|token_address)' 202001.tsv.gz \
  | gzip > /data/fabric-x-evm/integration/perf/testdata/dataset.tsv.gz

# Fetch USDC contract from mainnet
cd /data/fabric-x-evm
go run -tags=perf ./integration/perf -mode fetch

# Generate Go ABI bindings
go install github.com/ethereum/go-ethereum/cmd/abigen@latest
abigen --abi integration/perf/testdata/USDC_FiatTokenV2_2.abi \
       --pkg contracts --type FiatTokenV2_2 \
       --out integration/contracts/USDC_fiattokenv2_2.gen.go

# Pre-sign transactions
go run -tags=perf ./integration/perf -mode generate \
  -input ./integration/perf/testdata/dataset.tsv.gz
# Writes: integration/perf/testdata/USDC_dataset.json.gz (~27 MB)
```

Copy to dectrust6 and dectrust7 (run from dectrust8):

```bash
for dest in dectrust6.vpc.cloud9.ibm.com dectrust7.vpc.cloud9.ibm.com; do
  scp -p dectrust5.vpc.cloud9.ibm.com:/data/fabric-x-evm/integration/perf/testdata/USDC_dataset.json.gz \
          "$dest:/data/fabric-x-evm/integration/perf/testdata/"
  scp -p dectrust5.vpc.cloud9.ibm.com:/data/fabric-x-evm/integration/perf/testdata/USDC_contract.json \
          "$dest:/data/fabric-x-evm/integration/perf/testdata/"
  scp -p dectrust5.vpc.cloud9.ibm.com:/data/fabric-x-evm/integration/contracts/USDC_fiattokenv2_2.gen.go \
          "$dest:/data/fabric-x-evm/integration/contracts/"
done
```

---

## Reading the output

**Local (single replica):**
```
=== Summary ===
Transactions:  3000 successful, 0 failed, 0 skipped
Peak TPS:      NNN.NN tx/s
Overall TPS:   NNN.NN tx/s
Success rate:  100%
TPS stability (N samples): min=NNN.NN max=NNN.NN avg=NNN.NN stddev=NNN.NN CV=NN.N%

Demo passed (success rate 100%).
```

**Staging (3 replicas):**
```
Replica    Host                                Successful   Failed     TPS (overall)
-------    ----                                ----------   ------     -------------
1          dectrust5.vpc.cloud9.ibm.com        3000         0          NNN.NN
2          dectrust6.vpc.cloud9.ibm.com        3000         0          NNN.NN
3          dectrust7.vpc.cloud9.ibm.com        3000         0          NNN.NN

────────────────────────────────────────────────────────────────────────────────────
Aggregate TPS : ~NNN tx/s across 3 replicas
Total         : 9000 successful, 0 failed
────────────────────────────────────────────────────────────────────────────────────
```

**TPS stability / CV:** CV < 10% = very steady, 10–25% = moderate, >25% = high variance. Current runs show CV ≈ 30% — high. See "Throughput analysis" above.

---

## How the workload connects to Fabric-X on staging

`TestReplayJSONDataset` creates an **in-process gateway** rather than sending JSON-RPC calls to the deployed container. On staging it connects to the real Fabric-X backend (dectrust1–4).

The test loads its config from the file pointed to by `FABX_CONFIG_PATH`. `demo-staging.sh` sets:

```
FABX_CONFIG_PATH=/data/fabric-x-evm-test-config-N.yaml
```

`deploy-evm-staging.sh` generates that file alongside the container config. It has the same network/orderer/committer/identity settings as the deployed container but uses temp-dir DB paths (`/tmp/test-gateway-N.db`).

If `FABX_CONFIG_PATH` is not set, the test falls back to `integration/fabx.yaml` which points to a local `make start-x` network — only suitable for local development.

**Verifying real Fabric-X execution:** check that the synchronizer log shows `synchronizer ready at block N` with N > 0, and that `eth_blockNumber` increases during the run. A zero block-height delta means the Fabric-X nodes were idle and the test was not reaching the real backend.

---

## Troubleshooting

**`disk I/O error (6410)` in gateway logs** — SQLite can't find a temp dir; the release image is built `FROM scratch` with no `/tmp`. Fixed by `PRAGMA temp_store=MEMORY` on the `evm-gateway-demo` branch.

**Sync timeout on first start** — gateway syncs from block 0. If it times out, wait 30s and re-run `make start`.

**Staging container can't resolve hostnames** — must use `--network host`; Docker bridge DNS can't resolve `*.vpc.cloud9.ibm.com`. The deploy script already does this.

**Staging container write errors (`SQLITE_CANTOPEN`)** — deploy script uses `--user 0:0`. Don't change it.

**All transactions EVM-reverting (status=0x0 in receipts)** — most likely `NewSnapshot` bug: the endorser is reading state at block 0 (genesis) instead of the latest committed block. Ensure the branch is rebased on commit `605e744` or later.

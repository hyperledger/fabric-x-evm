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

## Repos and branches

| Repo | Branch | Purpose |
|---|---|---|
| `github.com/hyperledger/fabric-x-evm` | `evm-gateway-demo` | EVM gateway + workload test |
| `github.com/kushnireyal/fabric-x-ansible-collection` | `evm-gateway-demo` | Deployment scripts + Ansible role |

---

## Running locally (single replica)

The scripts work on both macOS and Linux. The only difference is `LOCAL_ANSIBLE_HOST`:
- **macOS** (Docker Desktop): use `host.docker.internal` — Docker Desktop can't reach the host via `localhost` inside containers
- **Linux** (Docker Engine): use `localhost` — Docker runs natively, no special hostname needed

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

**Run (current checkout):**

```bash
cd ~/.ansible/collections/ansible_collections/hyperledger/fabricx

# Full run — verbose output, tears down stack at the end
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm make demo-local  # macOS
LOCAL_ANSIBLE_HOST=localhost            EVM_REPO=/path/to/fabric-x-evm make demo-local  # Linux

# Quiet run — suppresses intermediate output, prints only section headers + final summary
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  make demo-local DEMO_ARGS=--quiet
```

Use `--quiet` when you care about the TPS number — without it the summary gets buried in verbose Ansible and test output.

**Run a specific branch or commit** — pass `EVM_BRANCH=`:

```bash
LOCAL_ANSIBLE_HOST=host.docker.internal \
  EVM_REPO=/path/to/fabric-x-evm EVM_BRANCH=my-feature make demo-local DEMO_ARGS=--quiet
```

**Fast iteration — `--warm` mode:**

`--warm` skips Fabric-X network setup (~2 min) and reuses the running stack. Only the EVM gateway is restarted, and only when the image SHA changed. Implies `--skip-teardown`.

```bash
# First run: full setup, leave the stack running
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  make demo-local DEMO_ARGS="--skip-teardown --quiet"

# All subsequent runs: reuse the stack, only restart gateway if code changed
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  make demo-local DEMO_ARGS="--warm --quiet"

# On a specific branch
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  make demo-local EVM_BRANCH=my-feature DEMO_ARGS="--warm --quiet"
```

If the stack is not running, `--warm` exits immediately with a clear error suggesting a full run first. To tear the stack down manually: `make teardown`.

The script builds a Docker image tagged `fabric-x-evm:<sha>` (skips build if that tag already exists), points `fabric-x-evm:dev` at it, then runs the workload. Exit 0 if success rate ≥ 95%.

**Increasing transaction count for stability measurement** — the default run replays 3000 transactions, which may complete in under one 2-second reporting tick and produce no stability data. Use `PERF_REPLAY_WRAP_COUNT` to replay the dataset window multiple times:

```bash
# Replay the 3000-tx window 10× = 30 000 transactions (~15+ s, enough for stability stats)
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  PERF_REPLAY_WRAP_COUNT=10 make demo-local DEMO_ARGS="--warm --quiet"
```

`PERF_REPLAY_WINDOW_SIZE=N` overrides the window size (default 3000; 0 = full dataset). `PERF_REPLAY_WRAP_COUNT=N` sets how many times to cycle through it. Both env vars are read directly by the Go test and are passed through automatically.

---

## Running on staging (3 replicas)

Staging topology: `dectrust1–4` run the Fabric-X network; `dectrust5–7` run EVM gateways; `dectrust8` is the control node (SSH jump host). All machines: 32 vCPUs, 64 GB RAM.

**From your Mac — one command runs everything:**

```bash
cd /path/to/fabric-x-evm

# Run on the current branch (default)
make run-demo

# Run on a specific branch
make run-demo EVM_BRANCH=my-feature

# Gateways already deployed + testdata already present
make run-demo DEMO_ARGS="--skip-testdata"
make run-demo EVM_BRANCH=my-feature DEMO_ARGS="--skip-testdata"

# Quiet run — suppresses intermediate output, prints only section headers + final summary
make run-demo DEMO_ARGS="--skip-testdata --quiet"
```

`make run-demo` defaults `EVM_BRANCH` to the current local branch. It SSHes to `dectrust8`, clones/syncs the Ansible collection there, checks out the requested EVM branch on each staging VM, builds a Docker image tagged `fabric-x-evm:<sha>` (skip if already present), then runs the workload.

**Image caching on staging:** each commit SHA is built once per host. Re-running the same branch or commit skips the gateway restart entirely — only new commits rebuild and restart.

**From dectrust8 directly:**

```bash
cd ~/fabric-x-ansible-collection   # or wherever it is cloned

# Deploy gateways and run workload (branch defaults to main)
make deploy-staging && make demo-staging DEMO_ARGS=--skip-deploy

# Deploy + run a specific branch
make deploy-staging EVM_BRANCH=my-feature
make demo-staging EVM_BRANCH=my-feature DEMO_ARGS=--skip-deploy

# Workload only (gateways already up, still syncs EVM repo to branch)
make demo-staging DEMO_ARGS=--skip-deploy

# Reset gateway state between runs (orderer untouched)
make ibmcloud-reset-state
```

---

## Testdata (staging blocker)

Three files are not in the git repo and must be generated on each of dectrust5–7 before the workload can run. `demo-staging.sh` and `run-demo.sh` will both print a clear error listing exactly which files are missing.

**Generate once on dectrust5, then copy to dectrust6/7.** `run-demo.sh` does this automatically if `--skip-testdata` is not passed. To do it manually:

```bash
ssh dectrust5.vpc.cloud9.ibm.com

# Step 1: Download Ethereum token transfer dataset (~10 GB, one-time)
cd /data
wget https://dataverse.harvard.edu/api/access/datafile/11691882 -O 202001.tsv.gz

# Step 2: Filter to USDC-only transfers
zgrep -E '(^.{93}a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48|token_address)' 202001.tsv.gz \
  | gzip > /data/fabric-x-evm/integration/perf/testdata/dataset.tsv.gz

# Step 3: Fetch USDC contract from mainnet
cd /data/fabric-x-evm
go run -tags=perf ./integration/perf -mode fetch

# Step 4: Generate Go ABI bindings
go install github.com/ethereum/go-ethereum/cmd/abigen@latest
abigen --abi integration/perf/testdata/USDC_FiatTokenV2_2.abi \
       --pkg contracts --type FiatTokenV2_2 \
       --out integration/contracts/USDC_fiattokenv2_2.gen.go

# Step 5: Pre-sign transactions
go run -tags=perf ./integration/perf -mode generate \
  -input ./integration/perf/testdata/dataset.tsv.gz
# Writes: integration/perf/testdata/USDC_dataset.json.gz (~27 MB)
```

Copy to dectrust6 and dectrust7 (run on dectrust8):

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

Both `demo-local.sh` and `demo-staging.sh` print a summary at the end:

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

- **Peak TPS** — highest windowed throughput during the run (recent window)
- **Overall TPS** — total transactions ÷ total elapsed time; most meaningful for comparing runs
- **TPS stability** — only printed when ≥ 2 windowed samples were collected (run needs to span >4 s); CV (coefficient of variation = stddev/mean) answers "is throughput steady?": CV < 10% is very steady, 10–25% is moderate, >25% is high variance
- **failed** — transactions the gateway rejected or that timed out waiting for finality
- **skipped** — entries in the dataset skipped by the workload generator (not sent)
- Exit 0 if success rate ≥ 95% (both local and staging)

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

Aggregate TPS is the sum of each replica's overall TPS.

---

## Troubleshooting

**`disk I/O error (6410)` in gateway logs** — SQLite can't find a temp dir; the release image is built `FROM scratch` with no `/tmp`. Fixed by `PRAGMA temp_store=MEMORY` on the `evm-gateway-demo` branch. If you see it, rebuild: `make build-image` then `make start`.

**Sync timeout on first start** — gateway syncs from block 0. If it times out, wait 30s and re-run `make start`.

**Staging container can't resolve hostnames** — must use `--network host`; Docker bridge DNS can't resolve `*.vpc.cloud9.ibm.com`. The deploy script already does this.

**Staging container write errors (`SQLITE_CANTOPEN`)** — `FROM scratch` image has no `/etc/passwd`, so named users fail. Deploy script uses `--user 0:0`. Don't change it.

**High failure rate under load** — MVCC conflicts spike when workers submit conflicting transactions to the same namespace. Use separate namespaces per replica (already done in staging) and tune `worker-count` in the gateway config.

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

Works on macOS and Linux. The only difference is `LOCAL_ANSIBLE_HOST`:
- **macOS** (Docker Desktop): `host.docker.internal`
- **Linux** (Docker Engine): `localhost`

**One-time setup:**

```bash
git clone https://github.com/hyperledger/fabric-x-evm.git
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

# First run: full setup, leave stack up
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  make demo-local DEMO_ARGS="--skip-teardown --quiet"

# Subsequent runs: reuse stack, only restart gateway if image SHA changed
LOCAL_ANSIBLE_HOST=host.docker.internal EVM_REPO=/path/to/fabric-x-evm \
  make demo-local DEMO_ARGS="--warm --quiet"
```

Use `EVM_BRANCH=my-feature` to run on a different branch. `--quiet` suppresses verbose Ansible/test output so the TPS summary isn't buried.

`PERF_REPLAY_WRAP_COUNT=N` cycles the window N times for longer runs (default 3000 txs may finish too fast for stable TPS samples). `PERF_REPLAY_WINDOW_SIZE=N` overrides the window size (0 = full dataset).

To tear down the stack: `make teardown`.

---

## Running on staging (3 replicas)

Staging topology: `dectrust1–4` run the Fabric-X network; `dectrust5–7` run EVM gateways; `dectrust8` is the SSH jump host. All machines: 32 vCPUs, 64 GB RAM.

From your Mac:

```bash
cd /path/to/fabric-x-evm
make run-demo                                              # current branch
make run-demo EVM_BRANCH=my-feature                        # specific branch
make run-demo DEMO_ARGS="--skip-testdata --skip-teardown --quiet"
```

`make run-demo` SSHes to dectrust8, syncs the Ansible collection there, checks out the EVM branch on each gateway VM, builds a Docker image tagged `fabric-x-evm:<sha>` (cached per SHA per host), then runs the workload. Re-running the same SHA skips the rebuild and gateway restart.

Always pass `--skip-teardown` to keep containers alive for subsequent `--skip-deploy` runs.

---

## Testdata

Three files must be present on each of dectrust5–7 before the workload runs. `run-demo.sh` generates and distributes them automatically unless `--skip-testdata` is passed.

To regenerate manually (on dectrust5):

```bash
cd /data
wget https://dataverse.harvard.edu/api/access/datafile/11691882 -O 202001.tsv.gz
zgrep -E '(^.{93}a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48|token_address)' 202001.tsv.gz \
  | gzip > /data/fabric-x-evm/integration/perf/testdata/dataset.tsv.gz

cd /data/fabric-x-evm
go run -tags=perf ./integration/perf -mode fetch
go install github.com/ethereum/go-ethereum/cmd/abigen@latest
abigen --abi integration/perf/testdata/USDC_FiatTokenV2_2.abi \
       --pkg contracts --type FiatTokenV2_2 \
       --out integration/contracts/USDC_fiattokenv2_2.gen.go
go run -tags=perf ./integration/perf -mode generate \
  -input ./integration/perf/testdata/dataset.tsv.gz
```

Then copy `USDC_dataset.json.gz`, `USDC_contract.json`, and `USDC_fiattokenv2_2.gen.go` to dectrust6 and dectrust7 at the same paths.

---

## Reading the output

Both demos print a summary at the end with successful/failed counts and TPS. Key numbers:

- **Overall TPS** — total transactions ÷ total elapsed; the headline number for comparing runs.
- **TPS stability CV** — coefficient of variation across windowed samples; <10% is steady, 10–25% moderate, >25% bursty.
- **Aggregate TPS** (staging) — sum of per-replica overall TPS.

Exit 0 if success rate ≥ 95%.

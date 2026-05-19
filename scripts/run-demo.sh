#!/usr/bin/env bash
# run-demo.sh — run the USDC staging demo from a developer's Mac.
#
# Orchestrates the full pipeline over SSH:
#   1. Checks local prerequisites
#   2. Clones or git-syncs the Ansible collection on the staging control node (dectrust8)
#   3. Generates testdata on dectrust5 if missing, distributes to dectrust6/7
#   4. Streams demo-staging.sh output in real time
#   5. Prints a summary
#
# Usage:
#   scripts/run-demo.sh [--staging-host HOST] [--ssh-user USER] \
#     [--evm-branch BRANCH] [--wrap-count N] [--skip-testdata] [--skip-deploy] \
#     [--skip-teardown] [--dry-run]
#
# Flags:
#   --staging-host HOST   SSH hostname of the control node
#                         (default: dectrust8.vpc.cloud9.ibm.com)
#   --ssh-user USER       SSH login user (default: root)
#   --evm-branch BRANCH   fabric-x-evm branch/ref to run (default: current branch)
#   --wrap-count N        replay the dataset window N times for stability measurement (default: 1)
#   --skip-testdata       skip testdata generation/distribution step
#   --skip-deploy         pass through to demo-staging.sh
#   --skip-teardown       pass through to demo-staging.sh
#   --perf-sweep          pass through to demo-staging.sh: run all worker configurations
#   --quiet               suppress intermediate output; print only section headers and final summary
#                         on failure the last 50 lines of the run log are printed to stderr
#   --dry-run             print commands without executing them

set -euo pipefail

# ─── defaults ─────────────────────────────────────────────────────────────────
STAGING_HOST="dectrust8.vpc.cloud9.ibm.com"
SSH_USER="root"
EVM_BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo main)"
WRAP_COUNT=1
SKIP_TESTDATA=false
SKIP_DEPLOY=false
SKIP_TEARDOWN=false
PERF_SWEEP=false
QUIET=false
DRY_RUN=false

# ─── argument parsing ─────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --staging-host)  STAGING_HOST="$2"; shift 2 ;;
        --ssh-user)      SSH_USER="$2"; shift 2 ;;
        --evm-branch)    EVM_BRANCH="$2"; shift 2 ;;
        --wrap-count)    WRAP_COUNT="$2"; shift 2 ;;
        --skip-testdata) SKIP_TESTDATA=true; shift ;;
        --skip-deploy)   SKIP_DEPLOY=true; shift ;;
        --skip-teardown) SKIP_TEARDOWN=true; shift ;;
        --perf-sweep)    PERF_SWEEP=true; shift ;;
        --quiet)         QUIET=true; shift ;;
        --dry-run)       DRY_RUN=true; shift ;;
        *) echo "Unknown flag: $1" >&2; exit 1 ;;
    esac
done

CTL="${SSH_USER}@${STAGING_HOST}"
EVM_DIR=/data/fabric-x-evm
TESTDATA_DIR="${EVM_DIR}/integration/perf/testdata"
EVM_DOMAIN=vpc.cloud9.ibm.com

# Ansible collection location on the control node (dectrust8)
FORK_URL="https://github.com/kushnireyal/fabric-x-ansible-collection.git"
FORK_BRANCH="evm-gateway-demo"
COLLECTION_ON_CTL="/home/${SSH_USER}/fabric-x-ansible-collection"
# root's home is /root, not /home/root
[[ "$SSH_USER" == "root" ]] && COLLECTION_ON_CTL="/root/fabric-x-ansible-collection"
DEMO_SCRIPT="${COLLECTION_ON_CTL}/scripts/demo-staging.sh"

log()     { echo "[$(date +'%H:%M:%S')] $*"; }
section() { echo; echo "=== $* ==="; }

# ─── quiet-mode setup ─────────────────────────────────────────────────────────
QUIET_LOG=""
if [[ "$QUIET" == true && "$DRY_RUN" != true ]]; then
    QUIET_LOG="$(mktemp /tmp/run-demo-quiet-$$.XXXXXX.log)"
    trap '
        exit_code=$?
        if [[ $exit_code -ne 0 ]]; then
            echo "=== Last 50 lines of run log ===" >&2
            tail -50 "$QUIET_LOG" >&2
        fi
        rm -f "$QUIET_LOG"
    ' EXIT
fi

vlog() {
    if [[ "$QUIET" == true && -n "$QUIET_LOG" ]]; then
        echo "[$(date +'%H:%M:%S')] $*" >> "$QUIET_LOG"
    else
        log "$@"
    fi
}

vrun() {
    if [[ "$QUIET" == true && -n "$QUIET_LOG" ]]; then
        "$@" >> "$QUIET_LOG" 2>&1
    else
        "$@"
    fi
}

# ─── step 1: prerequisites ────────────────────────────────────────────────────
section "STEP 1: Local prerequisites"

command -v ssh &>/dev/null || { echo "error: ssh not found" >&2; exit 1; }

if [[ "$DRY_RUN" != true ]]; then
    ssh -o ConnectTimeout=10 -o BatchMode=yes "$CTL" true 2>/dev/null \
        || { echo "error: cannot SSH to $CTL — check key authentication" >&2; exit 1; }
    vlog "SSH OK: $CTL"
else
    echo "+ [check SSH connectivity to $CTL]"
fi

# ─── step 2: sync Ansible collection on control node ─────────────────────────
section "STEP 2: Sync Ansible collection on $STAGING_HOST"

if [[ "$QUIET" != true ]]; then
    echo "+ [git clone/sync ${FORK_URL} branch ${FORK_BRANCH} at ${COLLECTION_ON_CTL}]"
fi
if [[ "$DRY_RUN" != true ]]; then
    vrun ssh "$CTL" "
        if [ -d '${COLLECTION_ON_CTL}/.git' ]; then
            cd '${COLLECTION_ON_CTL}'
            git fetch origin
            git checkout ${FORK_BRANCH}
            git reset --hard origin/${FORK_BRANCH}
        else
            git clone --branch ${FORK_BRANCH} ${FORK_URL} '${COLLECTION_ON_CTL}'
        fi
    "
    vlog "Collection synced at ${COLLECTION_ON_CTL}"
fi

# ─── step 3: testdata ─────────────────────────────────────────────────────────
section "STEP 3: Testdata"

if [[ "$SKIP_TESTDATA" == true ]]; then
    vlog "Skipping testdata step (--skip-testdata)"
else
    TESTDATA_FILES=(
        "${TESTDATA_DIR}/USDC_dataset.json.gz"
        "${TESTDATA_DIR}/USDC_contract.json"
        "${EVM_DIR}/integration/contracts/USDC_fiattokenv2_2.gen.go"
    )

    if [[ "$DRY_RUN" == true ]]; then
        echo "+ [check testdata on dectrust5.${EVM_DOMAIN}]"
        needs_generate=true
    else
        needs_generate=false
        for f in "${TESTDATA_FILES[@]}"; do
            if ! ssh "$CTL" "ssh dectrust5.${EVM_DOMAIN} '[ -f $f ]'" 2>/dev/null; then
                log "Missing on dectrust5: $f"
                needs_generate=true
            fi
        done
    fi

    if [[ "$needs_generate" == true ]]; then
        log "Generating testdata on dectrust5 (~20–40 min depending on download speed)"
        echo "+ [run testdata generation on dectrust5.${EVM_DOMAIN} via ${STAGING_HOST}]"
        if [[ "$DRY_RUN" != true ]]; then
            # Feed the generation script as stdin through dectrust8 → dectrust5.
            # EVM_BRANCH is passed as $1 so the remote script can sync to the right ref.
            ssh "$CTL" "ssh dectrust5.${EVM_DOMAIN} bash -s -- '${EVM_BRANCH}'" << 'GENSCRIPT'
set -euo pipefail
EVM_BRANCH="${1:-main}"
EVM_DIR=/data/fabric-x-evm
TESTDATA_DIR="${EVM_DIR}/integration/perf/testdata"
mkdir -p "${TESTDATA_DIR}"

echo "=== Syncing EVM repo to branch ${EVM_BRANCH} on dectrust5 ==="
if [ -d "${EVM_DIR}/.git" ]; then
    cd "${EVM_DIR}"
    git fetch origin 2>&1
    git checkout "${EVM_BRANCH}" 2>&1
    git reset --hard "origin/${EVM_BRANCH}" 2>&1
else
    git clone --branch "${EVM_BRANCH}" \
        https://github.com/hyperledger/fabric-x-evm.git "${EVM_DIR}" 2>&1
fi

echo "=== Testdata generation on dectrust5 ==="

# Step 1: Download Harvard Dataverse token transfer dataset (~10 GB)
if [ ! -f /data/202001.tsv.gz ]; then
    echo "Downloading Harvard Dataverse dataset..."
    wget -q --show-progress \
        https://dataverse.harvard.edu/api/access/datafile/11691882 \
        -O /data/202001.tsv.gz
else
    echo "Harvard dataset already downloaded."
fi

# Step 2: Filter to USDC-only transfers
if [ ! -f "${TESTDATA_DIR}/dataset.tsv.gz" ]; then
    echo "Filtering to USDC transfers..."
    zgrep -E '(^.{93}a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48|token_address)' \
        /data/202001.tsv.gz \
        | gzip > "${TESTDATA_DIR}/dataset.tsv.gz"
else
    echo "Filtered dataset already present."
fi

# Step 3: Fetch USDC contract bytecode and ABI from Ethereum mainnet
if [ ! -f "${TESTDATA_DIR}/USDC_contract.json" ]; then
    echo "Fetching USDC contract from mainnet..."
    cd "${EVM_DIR}"
    go run -tags=perf ./integration/perf -mode fetch
else
    echo "USDC_contract.json already present."
fi

# Step 4: Generate Go ABI bindings
if [ ! -f "${EVM_DIR}/integration/contracts/USDC_fiattokenv2_2.gen.go" ]; then
    echo "Generating Go bindings..."
    go install github.com/ethereum/go-ethereum/cmd/abigen@latest
    abigen --abi "${TESTDATA_DIR}/USDC_FiatTokenV2_2.abi" \
           --pkg contracts --type FiatTokenV2_2 \
           --out "${EVM_DIR}/integration/contracts/USDC_fiattokenv2_2.gen.go"
else
    echo "USDC_fiattokenv2_2.gen.go already present."
fi

# Step 5: Pre-generate signed transactions (~27 MB output)
if [ ! -f "${TESTDATA_DIR}/USDC_dataset.json.gz" ]; then
    echo "Pre-generating signed transactions..."
    cd "${EVM_DIR}"
    go run -tags=perf ./integration/perf -mode generate \
        -input ./integration/perf/testdata/dataset.tsv.gz
else
    echo "USDC_dataset.json.gz already present."
fi

echo "=== Testdata generation complete ==="
GENSCRIPT
        fi

        log "Distributing testdata from dectrust5 to dectrust6 and dectrust7..."
        echo "+ [scp testdata dectrust5 → dectrust6, dectrust7 via ${STAGING_HOST}]"
        if [[ "$DRY_RUN" != true ]]; then
            # Runs on dectrust8; single-quoted heredoc so $dest expands there.
            ssh "$CTL" << 'COPYEOF'
set -euo pipefail
TESTDATA_DIR=/data/fabric-x-evm/integration/perf/testdata
EVM_DIR=/data/fabric-x-evm
for dest in dectrust6.vpc.cloud9.ibm.com dectrust7.vpc.cloud9.ibm.com; do
    echo "Copying to $dest..."
    scp -p "dectrust5.vpc.cloud9.ibm.com:${TESTDATA_DIR}/USDC_dataset.json.gz" \
            "${dest}:${TESTDATA_DIR}/"
    scp -p "dectrust5.vpc.cloud9.ibm.com:${TESTDATA_DIR}/USDC_contract.json" \
            "${dest}:${TESTDATA_DIR}/"
    scp -p "dectrust5.vpc.cloud9.ibm.com:${EVM_DIR}/integration/contracts/USDC_fiattokenv2_2.gen.go" \
            "${dest}:${EVM_DIR}/integration/contracts/"
    echo "Done: $dest"
done
COPYEOF
        fi
    else
        log "Testdata present on dectrust5 — checking dectrust6/7..."
        if [[ "$DRY_RUN" != true ]]; then
            for dest_host in dectrust6.${EVM_DOMAIN} dectrust7.${EVM_DOMAIN}; do
                needs_copy=false
                for f in "${TESTDATA_FILES[@]}"; do
                    if ! ssh "$CTL" "ssh ${dest_host} '[ -f $f ]'" 2>/dev/null; then
                        needs_copy=true
                        break
                    fi
                done
                if [[ "$needs_copy" == true ]]; then
                    vlog "Copying missing testdata to ${dest_host}..."
                    vrun ssh "$CTL" "
                        scp -p dectrust5.${EVM_DOMAIN}:${TESTDATA_DIR}/USDC_dataset.json.gz \
                                ${dest_host}:${TESTDATA_DIR}/
                        scp -p dectrust5.${EVM_DOMAIN}:${TESTDATA_DIR}/USDC_contract.json \
                                ${dest_host}:${TESTDATA_DIR}/
                        scp -p dectrust5.${EVM_DOMAIN}:${EVM_DIR}/integration/contracts/USDC_fiattokenv2_2.gen.go \
                                ${dest_host}:${EVM_DIR}/integration/contracts/
                    "
                    vlog "Done: ${dest_host}"
                else
                    vlog "Testdata OK: ${dest_host}"
                fi
            done
        fi
    fi
fi

# ─── step 4: run demo ─────────────────────────────────────────────────────────
section "STEP 4: Run demo"

demo_args="--evm-branch ${EVM_BRANCH}"
[[ "$WRAP_COUNT"    != "1"  ]] && demo_args+=" --wrap-count ${WRAP_COUNT}"
[[ "$SKIP_DEPLOY"   == true ]] && demo_args+=" --skip-deploy"
[[ "$SKIP_TEARDOWN" == true ]] && demo_args+=" --skip-teardown"
[[ "$PERF_SWEEP"    == true ]] && demo_args+=" --perf-sweep"
[[ "$QUIET"         == true ]] && demo_args+=" --quiet"
[[ "$DRY_RUN"       == true ]] && demo_args+=" --dry-run"

if [[ "$QUIET" != true ]]; then
    echo "+ ssh -tt $CTL 'bash ${DEMO_SCRIPT}${demo_args:+ $demo_args}'"
fi
if [[ "$DRY_RUN" != true ]]; then
    LOCAL_LOG=$(mktemp /tmp/run-demo-$$.XXXXXX.log)
    set +e
    if [[ "$QUIET" == true ]]; then
        # Use a while-read loop instead of tee|awk to avoid block-buffering in the pipe chain.
        # tee uses stdio block buffering when writing to a pipe, which holds progress lines
        # until the buffer fills (~4-8 KB). while-read is line-by-line and flushes immediately.
        ssh -tt "$CTL" "bash ${DEMO_SCRIPT}${demo_args:+ $demo_args}" 2>&1 | \
            while IFS= read -r line; do
                line="${line%$'\r'}"  # strip CR from PTY output
                echo "$line" >> "$QUIET_LOG"
                echo "$line" >> "$LOCAL_LOG"
                [[ "$line" == *"[replica "* ]] && echo "$line"
            done
    else
        ssh -tt "$CTL" "bash ${DEMO_SCRIPT}${demo_args:+ $demo_args}" 2>&1 | tee "$LOCAL_LOG"
    fi
    demo_exit=${PIPESTATUS[0]}
    set -e
fi

# ─── step 5: summary ──────────────────────────────────────────────────────────
section "STEP 5: Summary"

if [[ "$DRY_RUN" != true ]]; then
    echo ""
    # Print the full results table (replica rows, stability lines, aggregate) from STEP 6.
    awk '/=== STEP 6: Results ===/{p=1;next} /=== STEP 7:/{p=0} p' "$LOCAL_LOG" || true
    grep -E "Demo (passed|FAIL)" "$LOCAL_LOG" || true
    echo ""
    rm -f "$LOCAL_LOG"
    if [[ "${demo_exit:-1}" -eq 0 ]]; then
        log "Demo PASSED"
    else
        log "Demo FAILED (exit code $demo_exit)"
    fi
    exit "${demo_exit:-1}"
else
    echo "(dry-run complete)"
    exit 0
fi

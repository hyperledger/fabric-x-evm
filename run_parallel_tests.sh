#!/bin/bash

# Script to run parallel performance tests in tmux
# Creates a tmux session with N windows, each running tests with a different namespace
# Usage: ./run_parallel_tests.sh [num_namespaces]
# Example: ./run_parallel_tests.sh 10

SESSION_NAME="perf-tests"

# Get number of namespaces from first argument, default to 10
NUM_NAMESPACES="${1:-10}"

# Validate input
if ! [[ "$NUM_NAMESPACES" =~ ^[0-9]+$ ]] || [ "$NUM_NAMESPACES" -lt 1 ]; then
    echo "Error: Number of namespaces must be a positive integer"
    echo "Usage: $0 [num_namespaces]"
    exit 1
fi

echo "Creating $NUM_NAMESPACES parallel test windows with namespaces base000-base$(printf '%03d' $((NUM_NAMESPACES-1)))"

# Check if tmux session already exists
if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    echo "Tmux session '$SESSION_NAME' already exists. Attaching to it..."
    tmux attach-session -t "$SESSION_NAME"
    exit 0
fi

# Create new tmux session (detached)
tmux new-session -d -s "$SESSION_NAME" -n "test-0"

# Run test in the first window (namespace base000)
# Note: The test will create all namespaces programmatically using the -namespaces flag
tmux send-keys -t "$SESSION_NAME:0" "go test -timeout 10000s -tags=perf -run ^TestReplayJSONDataset$ -v -count=1 ./integration/perf/... -gateway-config fabx-full.yaml -outstanding 1000 -dataset integration/perf/testdata/USDC_dataset.012020.json.gz -enable-metrics -namespace base000 -namespaces $NUM_NAMESPACES" C-m

# Create remaining windows (namespaces base001-baseNNN)
for ((i=1; i<NUM_NAMESPACES; i++)); do
    # Create new window
    tmux new-window -t "$SESSION_NAME:$i" -n "test-$i"
    
    # Namespace name: base000, base001, base002, etc.
    namespace=$(printf "base%03d" $i)
    
    # Run test in this window (no -namespaces flag needed, they're already created by first test)
    tmux send-keys -t "$SESSION_NAME:$i" "go test -timeout 10000s -tags=perf -run ^TestReplayJSONDataset$ -v -count=1 ./integration/perf/... -gateway-config fabx-full.yaml -outstanding 1000 -dataset integration/perf/testdata/USDC_dataset.012020.json.gz -enable-metrics -namespace $namespace" C-m
done

# Select the first window
tmux select-window -t "$SESSION_NAME:0"

echo "Tmux session '$SESSION_NAME' created with $NUM_NAMESPACES windows running parallel tests."
echo "Namespaces: base000 through base$(printf '%03d' $((NUM_NAMESPACES-1)))"
echo ""
echo "Attach to the session with: tmux attach-session -t $SESSION_NAME"
echo "Navigate between windows with: Ctrl-b n (next) or Ctrl-b p (previous)"
echo "Or use: Ctrl-b 0-9 to jump to a specific window"
echo ""
echo "To detach from the session: Ctrl-b d"
echo "To kill the session: tmux kill-session -t $SESSION_NAME"

# Attach to the session
tmux attach-session -t "$SESSION_NAME"

# Made with Bob

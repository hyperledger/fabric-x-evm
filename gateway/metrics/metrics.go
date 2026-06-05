// Package metrics holds the EVM-side (workload generator + in-process gateway) Prometheus
// instrumentation used to diagnose throughput regressions vs a mock harness baseline.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// LoadgenSendTxTotal counts every wrappedGateway.SendTransaction call made by the
	// workload generator. labelled by outcome so callers can compute success rate.
	LoadgenSendTxTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_loadgen_sendtx_total",
		Help: "Total SendTransaction calls from the workload generator, by outcome",
	}, []string{"outcome"})

	// LoadgenSendTxDuration is the wall-clock duration of SendTransaction from the
	// loadgen's POV. captures any blocking inside the in-process gateway (TxQueue full,
	// signing, validation).
	LoadgenSendTxDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_loadgen_sendtx_duration_seconds",
		Help:    "Wall-clock duration of SendTransaction from the workload generator",
		Buckets: []float64{0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	// LoadgenCommittedTotal counts notifications/completions back to the workload
	// generator (whatever path is used: TransactionByHash poll, notification tracker, etc).
	LoadgenCommittedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_loadgen_committed_total",
		Help: "Total tx completions observed by the workload generator, by result",
	}, []string{"result"})

	// LoadgenCommittedLatency is per-tx end-to-end latency from submit to commit-notification.
	// only populated by closed-loop mode (open-loop doesn't track per-tx completion timing).
	LoadgenCommittedLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_loadgen_committed_latency_seconds",
		Help:    "Time from SendTransaction to commit-notification (closed-loop only)",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	})

	// LoadgenInflight is the count of tx currently submitted but not yet completed.
	LoadgenInflight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "evm_loadgen_inflight",
		Help: "Transactions submitted but not yet completed",
	})

	// GatewayTxQueueSize tracks the current depth of the in-process gateway's TxQueue.
	GatewayTxQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "evm_gateway_txqueue_size",
		Help: "Current depth of the in-process gateway TxQueue ready list",
	})

	// GatewayTxQueueWait is the time a tx spent waiting between Enqueue and Dequeue.
	GatewayTxQueueWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_txqueue_wait_seconds",
		Help:    "Time a tx waits in the gateway's TxQueue between Enqueue and Dequeue",
		Buckets: []float64{0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	// GatewayTxQueueEnqueueDuration is wall-clock time spent INSIDE Enqueue.
	// suspect: TxQueueV2's dependency tracking iterates participantMap which holds
	// N pointers when queue is full. If this duration grows with queue depth →
	// that's the throughput cap.
	GatewayTxQueueEnqueueDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_txqueue_enqueue_duration_seconds",
		Help:    "Wall-clock duration of TxQueue.Enqueue (including dependency-graph rebuild)",
		Buckets: []float64{0.0000001, 0.000001, 0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// GatewayTxQueueDequeueDuration is wall-clock time spent INSIDE Dequeue.
	// includes any time waiting on the cond variable when no ready tx is available.
	// if Enqueue is the cap, Dequeue will mostly be cond-wait time when the worker is
	// not falling behind.
	GatewayTxQueueDequeueDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_txqueue_dequeue_duration_seconds",
		Help:    "Wall-clock duration of TxQueue.Dequeue (including cond wait when empty)",
		Buckets: []float64{0.0000001, 0.000001, 0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// GatewayTxQueueDequeueLockWait is time spent acquiring q.mu.Lock at the start of Dequeue.
	// growing under load = lock contention (Enqueue / Complete holding the lock).
	GatewayTxQueueDequeueLockWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_txqueue_dequeue_lock_wait_seconds",
		Help:    "Time spent in q.mu.Lock() at the start of Dequeue (lock contention)",
		Buckets: []float64{0.0000001, 0.000001, 0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// GatewayTxQueueDequeueCondWait is time spent in cond.Wait() because readyList was empty.
	// only observed if the wait actually happened (no observation if Dequeue returned immediately).
	// large = the queue is starved of ready txs (waiting on Complete to promote next).
	GatewayTxQueueDequeueCondWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_txqueue_dequeue_cond_wait_seconds",
		Help:    "Time spent in cond.Wait() inside Dequeue (readyList was empty, waiting for Complete to promote)",
		Buckets: []float64{0.0000001, 0.000001, 0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// GatewayTxQueueCompleteDuration is wall-clock time spent in TxQueue.Complete (including
	// lock acquisition + dependency promotion). Holds the lock the whole time → blocks Dequeue.
	GatewayTxQueueCompleteDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_txqueue_complete_duration_seconds",
		Help:    "Wall-clock duration of TxQueue.Complete (lock acquire + dependency promotion)",
		Buckets: []float64{0.0000001, 0.000001, 0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// GatewayWorkerChanSendDuration is time blocked in `g.endorsementChan <- end` inside processTx.
	// non-zero = BatchSubmitter is back-pressuring the worker.
	GatewayWorkerChanSendDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_worker_chan_send_seconds",
		Help:    "Time blocked sending endorsement to BatchSubmitter chan (downstream back-pressure)",
		Buckets: []float64{0.0000001, 0.000001, 0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// GatewayWorkerCycleDuration is full time from Dequeue-start to next Dequeue-start.
	// 1 / mean = max worker throughput.
	GatewayWorkerCycleDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_worker_cycle_seconds",
		Help:    "Wall-clock time per worker iteration (Dequeue → processTx → next Dequeue start)",
		Buckets: []float64{0.0000001, 0.000001, 0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// GatewayEndorseDuration is the time spent in processTx (ExecuteEthTx, i.e. the
	// EVM execution + endorsement step), per tx.
	GatewayEndorseDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_endorse_duration_seconds",
		Help:    "Time spent in processTx (ExecuteEthTx) per transaction",
		Buckets: []float64{0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	// GatewaySubmitDuration is the time spent in the SDK Submit call (gateway → orderer).
	// includes any per-orderer Broadcast serialization or mutex wait.
	GatewaySubmitDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evm_gateway_submit_duration_seconds",
		Help:    "Time spent in submitter.Submit per transaction",
		Buckets: []float64{0.00001, 0.0001, 0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})
)

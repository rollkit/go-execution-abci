package adapter

import (
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/discard"
	prometheus "github.com/go-kit/kit/metrics/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
)

const (
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this package.
	MetricsSubsystem = "adapter"
)

// Metrics contains metrics exposed by this package.
// Each metric is annotated with `metrics_labels` tag, which lists the labels
// used by the metric.
type Metrics struct {
	// Tx Validation
	TxValidationTotal       metrics.Counter
	TxValidationResultTotal metrics.Counter `metrics_labels:"result"`
	CheckTxDurationSeconds  metrics.Histogram

	// InitChain
	InitChainDurationSeconds metrics.Histogram

	// Block Execution
	BlockExecutionDurationSeconds     metrics.Histogram
	TxsExecutedPerBlock               metrics.Histogram
	BlockExecutionStepDurationSeconds metrics.Histogram `metrics_labels:"step"`
	ValidatorUpdatesTotal             metrics.Counter
	ConsensusParamUpdatesTotal        metrics.Counter

	// Tx Retrieval
	GetTxsDurationSeconds          metrics.Histogram
	TxsProposedTotal               metrics.Counter
	MempoolReapDurationSeconds     metrics.Histogram
	PrepareProposalDurationSeconds metrics.Histogram
}

// PrometheusMetrics returns Metrics build using Prometheus client library.
// Optionally, labels can be provided along with their values ("foo",
// "fooValue").
func PrometheusMetrics(namespace string, labelsAndValues ...string) *Metrics {
	labels := []string{}
	for i := 0; i < len(labelsAndValues); i += 2 {
		labels = append(labels, labelsAndValues[i])
	}

	return &Metrics{
		// Tx Validation
		TxValidationTotal: prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "tx_validation_total",
			Help:      "Total number of transactions received for validation.",
		}, labels).With(labelsAndValues...),
		TxValidationResultTotal: prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "tx_validation_result_total",
			Help:      "Total number of transaction validation results by outcome.",
		}, append(labels, "result")).With(labelsAndValues...),
		CheckTxDurationSeconds: prometheus.NewHistogramFrom(stdprometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "checktx_duration_seconds",
			Help:      "Latency of the Mempool.CheckTx call.",
			Buckets:   stdprometheus.DefBuckets,
		}, labels).With(labelsAndValues...),

		// InitChain
		InitChainDurationSeconds: prometheus.NewHistogramFrom(stdprometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "initchain_duration_seconds",
			Help:      "Time taken for InitChain.",
			Buckets:   stdprometheus.DefBuckets,
		}, labels).With(labelsAndValues...),

		// Block Execution
		BlockExecutionDurationSeconds: prometheus.NewHistogramFrom(stdprometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "block_execution_duration_seconds",
			Help:      "Total time taken for ExecuteTxs.",
			Buckets:   stdprometheus.ExponentialBuckets(0.1, 2, 10), // 100ms to ~51s
		}, labels).With(labelsAndValues...),
		TxsExecutedPerBlock: prometheus.NewHistogramFrom(stdprometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "txs_executed_per_block",
			Help:      "Number of transactions included in executed blocks.",
			Buckets:   stdprometheus.ExponentialBuckets(1, 2, 10), // 1 to 512
		}, labels).With(labelsAndValues...),
		BlockExecutionStepDurationSeconds: prometheus.NewHistogramFrom(stdprometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "block_execution_step_duration_seconds",
			Help:      "Duration of specific steps within ExecuteTxs.",
			Buckets:   stdprometheus.DefBuckets,
		}, append(labels, "step")).With(labelsAndValues...),

		ValidatorUpdatesTotal: prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "validator_updates_total",
			Help:      "Total number of validator updates processed.",
		}, labels).With(labelsAndValues...),
		ConsensusParamUpdatesTotal: prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "consensus_param_updates_total",
			Help:      "Total number of consensus parameter updates processed.",
		}, labels).With(labelsAndValues...),

		// Tx Retrieval
		GetTxsDurationSeconds: prometheus.NewHistogramFrom(stdprometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "get_txs_duration_seconds",
			Help:      "Time taken for GetTxs.",
			Buckets:   stdprometheus.DefBuckets,
		}, labels).With(labelsAndValues...),
		TxsProposedTotal: prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "txs_proposed_total",
			Help:      "Total number of transactions returned by GetTxs.",
		}, labels).With(labelsAndValues...),
		MempoolReapDurationSeconds: prometheus.NewHistogramFrom(stdprometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "mempool_reap_duration_seconds",
			Help:      "Time taken for Mempool.ReapMaxBytesMaxGas.",
			Buckets:   stdprometheus.DefBuckets,
		}, labels).With(labelsAndValues...),
		PrepareProposalDurationSeconds: prometheus.NewHistogramFrom(stdprometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: MetricsSubsystem,
			Name:      "prepare_proposal_duration_seconds",
			Help:      "Time taken for App.PrepareProposal.",
			Buckets:   stdprometheus.DefBuckets,
		}, labels).With(labelsAndValues...),
	}
}

// NopMetrics returns no-op Metrics.
func NopMetrics() *Metrics {
	return &Metrics{
		// Tx Validation
		TxValidationTotal:       discard.NewCounter(),
		TxValidationResultTotal: discard.NewCounter(),
		CheckTxDurationSeconds:  discard.NewHistogram(),

		// InitChain
		InitChainDurationSeconds: discard.NewHistogram(),

		// Block Execution
		BlockExecutionDurationSeconds:     discard.NewHistogram(),
		TxsExecutedPerBlock:               discard.NewHistogram(),
		BlockExecutionStepDurationSeconds: discard.NewHistogram(),
		ValidatorUpdatesTotal:             discard.NewCounter(),
		ConsensusParamUpdatesTotal:        discard.NewCounter(),

		// Tx Retrieval
		GetTxsDurationSeconds:          discard.NewHistogram(),
		TxsProposedTotal:               discard.NewCounter(),
		MempoolReapDurationSeconds:     discard.NewHistogram(),
		PrepareProposalDurationSeconds: discard.NewHistogram(),
	}
}

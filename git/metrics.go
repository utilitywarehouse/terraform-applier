package git

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// lastSyncTimestamp is a Gauge that captures the timestamp of the last
	// successful git sync
	lastSyncTimestamp *prometheus.GaugeVec
	// syncCount is a Counter vector of git sync operations
	syncCount *prometheus.CounterVec
	// syncLatency is a Histogram vector that keeps track of git repo sync durations
	syncLatency *prometheus.HistogramVec
)

func EnableMetrics(metricsNamespace string, registerer prometheus.Registerer) {
	lastSyncTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "git_last_sync_timestamp",
		Help:      "Timestamp of the last successful git sync",
	},
		[]string{
			// name of the repository
			"repo",
		},
	)

	syncCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "git_sync_count",
		Help:      "Count of git sync operations",
	},
		[]string{
			// name of the repository
			"repo",
			// Whether the apply was successful or not
			"success",
		},
	)

	syncLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "git_sync_latency_seconds",
		Help:      "Latency for git repo sync",
		Buckets:   []float64{1, 5, 10, 20, 30, 60, 90, 120, 150, 300},
	},
		[]string{
			// name of the repository
			"repo",
		},
	)

	registerer.MustRegister(
		lastSyncTimestamp,
		syncCount,
		syncLatency,
	)
}

// recordGitSync records a git repository sync attempt by updating all the
// relevant metrics
func recordGitSync(repo string, success bool) {
	// if metrics not enabled return
	if lastSyncTimestamp == nil || syncCount == nil {
		return
	}
	if success {
		lastSyncTimestamp.With(prometheus.Labels{
			"repo": repo,
		}).Set(float64(time.Now().Unix()))
	}
	syncCount.With(prometheus.Labels{
		"repo":    repo,
		"success": strconv.FormatBool(success),
	}).Inc()
}

func updateSyncLatency(repo string, start time.Time) {
	// if metrics not enabled return
	if syncLatency == nil {
		return
	}
	syncLatency.WithLabelValues(repo).Observe(time.Since(start).Seconds())
}

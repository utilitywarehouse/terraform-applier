package metrics

import (
	"context"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsNamespace = "terraform_applier"
)

//go:generate go run github.com/golang/mock/mockgen -package metrics -destination mock_prometheus.go github.com/utilitywarehouse/terraform-applier/metrics PrometheusInterface

// PrometheusInterface allows for mocking out the functionality of Prometheus when testing the full process of an apply run.
type PrometheusInterface interface {
	UpdateModuleSuccess(string, string, string, bool)
	UpdateModuleRunDuration(string, string, string, float64, bool)
	SetRunPending(string, string, bool)
}

// Prometheus implements instrumentation of metrics for terraform-applier.
// terraformExitCodeCount is a Counter vector to increment the number of exit codes for each terraform execution
// moduleRunCount is a Counter vector to increment the number of successful and failed run attempts for each module.
// moduleRunDuration is a Summary vector that keeps track of the duration for runs.
// moduleRunSuccess is the last run outcome of the module run.
// moduleRunning is the number of modules currently in running state.
type Prometheus struct {
	moduleRunCount     *prometheus.CounterVec
	moduleRunDuration  *prometheus.HistogramVec
	moduleRunPending   *prometheus.GaugeVec
	moduleRunSuccess   *prometheus.GaugeVec
	moduleRunTimestamp *prometheus.GaugeVec
	moduleInfo         *prometheus.GaugeVec
}

// Init creates and registers the custom metrics for terraform-applier.
func (p *Prometheus) Init() {
	p.moduleInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "module_info",
		Help:      "Current information about module including status",
	},
		[]string{
			"module",
			// Namespace name of the module that was ran
			"namespace",
			// state of the module
			"state",
			// potential reason associated with current state
			"reason",
		},
	)

	p.moduleRunCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "module_run_count",
		Help:      "Success metric for every module run",
	},
		[]string{
			// Name of the module that was ran
			"module",
			// Namespace name of the module that was ran
			"namespace",
			// Type of the last run
			"run_type",
			// Result: true if the run was successful, false otherwise
			"success",
		},
	)
	p.moduleRunDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "module_run_duration_seconds",
		Help:      "Duration of the terraform run for a module",

		// default Histogram bucket (.005s-10s) is not suitable for typical terraform run duration
		Buckets: []float64{30, 60, 120, 180, 240, 300, 600, 900, 1200, 1500, 1800},
	},
		[]string{
			// Name of the module that was ran
			"module",
			// Namespace name of the module that was ran
			"namespace",
			// Type of the last run
			"run_type",
			// Result: true if the run was successful, false otherwise
			"success",
		},
	)
	p.moduleRunPending = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "module_run_pending",
		Help:      "is module ready to run but not yet started run?",
	},
		[]string{
			// Name of the module
			"module",
			// Namespace name of the module that was ran
			"namespace",
		},
	)
	p.moduleRunSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "module_last_run_success",
		Help:      "Was the last terraform run for this module successful?",
	},
		[]string{
			"module",
			// Namespace name of the module that was ran
			"namespace",
			// Type of the last run
			"run_type",
		},
	)
	p.moduleRunTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "module_last_run_timestamp",
		Help:      "Timestamp of the last successful module run",
	},
		[]string{
			"module",
			// Namespace name of the module that was ran
			"namespace",
			// Type of the last run
			"run_type",
		},
	)

	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		p.moduleRunCount,
		p.moduleRunDuration,
		p.moduleRunSuccess,
		p.moduleRunPending,
		p.moduleRunTimestamp,
		p.moduleInfo,
	)

}

// UpdateModuleSuccess increments the given module's Counter for either successful or failed run attempts.
func (p *Prometheus) UpdateModuleSuccess(module, namespace, runType string, success bool) {
	if success {
		p.moduleRunTimestamp.With(prometheus.Labels{
			"module":    module,
			"namespace": namespace,
			"run_type":  runType,
		}).Set(float64(time.Now().Unix()))
	}
	p.moduleRunCount.With(prometheus.Labels{
		"module":    module,
		"namespace": namespace,
		"run_type":  runType,
		"success":   strconv.FormatBool(success),
	}).Inc()
	p.setRunSuccess(module, namespace, runType, success)
}

// UpdateModuleRunDuration adds a data point (latency of the most recent run) to the module_apply_duration_seconds Summary metric, with a tag indicating whether or not the run was successful.
func (p *Prometheus) UpdateModuleRunDuration(module, namespace, runType string, runDuration float64, success bool) {
	p.moduleRunDuration.With(prometheus.Labels{
		"module":    module,
		"namespace": namespace,
		"run_type":  runType,
		"success":   strconv.FormatBool(success),
	}).Observe(runDuration)
}

// setRunSuccess sets last run outcome for a module
func (p *Prometheus) setRunSuccess(module, namespace, runType string, success bool) {
	as := float64(0)
	if success {
		as = 1
	}
	p.moduleRunSuccess.With(prometheus.Labels{
		"module":    module,
		"namespace": namespace,
		"run_type":  runType,
	}).Set(as)
}

// setRunPending sets pending status for a module
func (p *Prometheus) SetRunPending(module, namespace string, pending bool) {
	as := float64(0)
	if pending {
		as = 1
	}
	p.moduleRunPending.With(prometheus.Labels{
		"module":    module,
		"namespace": namespace,
	}).Set(as)
}

// CollectModuleInfo when called resets 'module_info' and collect current state of the modules
func (p *Prometheus) CollectModuleInfo(ctx context.Context, kc client.Client) error {

	kubeModuleList := &tfaplv1beta1.ModuleList{}
	if err := kc.List(ctx, kubeModuleList); err != nil {
		return err
	}

	// reset all values and re-set current value
	p.moduleInfo.Reset()

	for _, m := range kubeModuleList.Items {
		p.moduleInfo.With(prometheus.Labels{
			"module":    m.Name,
			"namespace": m.Namespace,
			"state":     m.Status.CurrentState,
			"reason":    m.Status.StateReason,
		}).Set(1)
	}
	return nil
}

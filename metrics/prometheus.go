package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsNamespace = "terraform_applier"
)

//go:generate go run github.com/golang/mock/mockgen -package metrics -destination mock_prometheus.go github.com/utilitywarehouse/terraform-applier/metrics PrometheusInterface

// PrometheusInterface allows for mocking out the functionality of Prometheus when testing the full process of an apply run.
type PrometheusInterface interface {
	UpdateTerraformExitCodeCount(string, string, string, int)
	UpdateModuleSuccess(string, string, bool)
	UpdateModuleRunDuration(string, string, float64, bool)
	IncRunningModuleCount(string)
	DecRunningModuleCount(string)
}

// Prometheus implements instrumentation of metrics for terraform-applier.
// terrafromExitCodeCount is a Counter vector to increment the number of exit codes for each terraform execution
// moduleRunCount is a Counter vector to increment the number of successful and failed run attempts for each module.
// moduleRunDuration is a Summary vector that keeps track of the duration for runs.
// moduleRunSuccess is the last run outcome of the module run.
// moduleRunning is the number of modules currently in running state.
type Prometheus struct {
	terraformExitCodeCount *prometheus.CounterVec
	moduleRunCount         *prometheus.CounterVec
	moduleRunDuration      *prometheus.HistogramVec
	moduleRunSuccess       *prometheus.GaugeVec
	runningModuleCount     *prometheus.GaugeVec
}

// Init creates and registers the custom metrics for terraform-applier.
func (p *Prometheus) Init() {
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
			// Result: true if the run was successful, false otherwise
			"success",
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
		},
	)
	p.terraformExitCodeCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "terraform_exit_code_count",
		Help:      "Count of terraform exit codes",
	},
		[]string{
			// Name of the module that was ran
			"module",
			// Namespace name of the module that was ran
			"namespace",
			// plan, apply, init etc
			"command",
			// Exit code
			"exit_code",
		},
	)

	p.runningModuleCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "running_module_count",
		Help:      "The total number of modules in running state",
	},
		[]string{
			// Namespace name of the module that was ran
			"namespace",
		},
	)

	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		p.moduleRunCount,
		p.moduleRunDuration,
		p.moduleRunSuccess,
		p.terraformExitCodeCount,
		p.runningModuleCount,
	)

}

// UpdateTerraformExitCodeCount increments for each exit code returned by terraform
func (p *Prometheus) UpdateTerraformExitCodeCount(module, namespace string, cmd string, code int) {
	p.terraformExitCodeCount.With(prometheus.Labels{
		"module":    module,
		"namespace": namespace,
		"command":   cmd,
		"exit_code": strconv.Itoa(code),
	}).Inc()
}

// UpdateModuleSuccess increments the given module's Counter for either successful or failed run attempts.
func (p *Prometheus) UpdateModuleSuccess(module, namespace string, success bool) {
	p.moduleRunCount.With(prometheus.Labels{
		"module":    module,
		"namespace": namespace,
		"success":   strconv.FormatBool(success),
	}).Inc()
	p.setApplySuccess(module, namespace, success)
}

// UpdateModuleRunDuration adds a data point (latency of the most recent run) to the module_apply_duration_seconds Summary metric, with a tag indicating whether or not the run was successful.
func (p *Prometheus) UpdateModuleRunDuration(module, namespace string, runDuration float64, success bool) {
	p.moduleRunDuration.With(prometheus.Labels{
		"module":    module,
		"namespace": namespace,
		"success":   strconv.FormatBool(success),
	}).Observe(runDuration)
}

// setApplySuccess sets last run outcome for a module
func (p *Prometheus) setApplySuccess(module, namespace string, success bool) {
	as := float64(0)
	if success {
		as = 1
	}
	p.moduleRunSuccess.With(prometheus.Labels{
		"module":    module,
		"namespace": namespace,
	}).Set(as)
}

func (p *Prometheus) IncRunningModuleCount(namespace string) {
	p.runningModuleCount.With(prometheus.Labels{
		"namespace": namespace,
	}).Inc()
}

func (p *Prometheus) DecRunningModuleCount(namespace string) {
	p.runningModuleCount.With(prometheus.Labels{
		"namespace": namespace,
	}).Dec()
}

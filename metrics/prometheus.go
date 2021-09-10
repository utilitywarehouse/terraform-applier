package metrics

import (
	"path/filepath"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricsNamespace = "terraform_applier"
)

// PrometheusInterface allows for mocking out the functionality of Prometheus when testing the full process of an apply run.
type PrometheusInterface interface {
	UpdateTerraformExitCodeCount(string, string, int)
	UpdateModuleSuccess(string, bool)
	UpdateModuleApplyDuration(string, float64, bool)
}

// Prometheus implements instrumentation of metrics for terraform-applier.
// terrafromExitCodeCount is a Counter vector to increment the number of exit codes for each terraform execution
// moduleApplyCount is a Counter vector to increment the number of successful and failed apply attempts for each module.
// runLatency is a Summary vector that keeps track of the duration for apply runs.
type Prometheus struct {
	terraformExitCodeCount *prometheus.CounterVec
	moduleApplyCount       *prometheus.CounterVec
	moduleApplyDuration    *prometheus.HistogramVec
}

// Init creates and registers the custom metrics for kube-applier.
func (p *Prometheus) Init() {

	p.moduleApplyCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "module_apply_count",
		Help:      "Success metric for every module applied",
	},
		[]string{
			// Path of the module that was applied
			"module",
			// Result: true if the apply was successful, false otherwise
			"success",
		},
	)
	p.moduleApplyDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "module_apply_duration_seconds",
		Help:      "Duration of the apply runs",
	},
		[]string{
			// Path of the module that was applied
			"module",
			// Result: true if the run was successful, false otherwise
			"success",
		},
	)
	p.terraformExitCodeCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "terraform_exit_code_count",
		Help:      "Count of terraform exit codes",
	},
		[]string{
			// Path of the module that was applied
			"module",
			// plan, apply, init etc
			"command",
			// Exit code
			"exit_code",
		},
	)
	prometheus.MustRegister(p.moduleApplyCount)
	prometheus.MustRegister(p.moduleApplyDuration)
	prometheus.MustRegister(p.terraformExitCodeCount)

}

// UpdateTerraformExitCodeCount increments for each exit code returned by terraform
func (p *Prometheus) UpdateTerraformExitCodeCount(module string, cmd string, code int) {
	p.terraformExitCodeCount.With(prometheus.Labels{
		"module":    filepath.Base(module),
		"command":   cmd,
		"exit_code": strconv.Itoa(code),
	}).Inc()
}

// UpdateModuleSuccess increments the given module's Counter for either successful apply attempts or failed apply attempts.
func (p *Prometheus) UpdateModuleSuccess(module string, success bool) {
	p.moduleApplyCount.With(prometheus.Labels{
		"module": filepath.Base(module), "success": strconv.FormatBool(success),
	}).Inc()
}

// UpdateModuleApplyDuration adds a data point (latency of the most recent run) to the module_apply_duration_seconds Summary metric, with a tag indicating whether or not the run was successful.
func (p *Prometheus) UpdateModuleApplyDuration(module string, runDuration float64, success bool) {
	p.moduleApplyDuration.With(prometheus.Labels{
		"module":  filepath.Base(module),
		"success": strconv.FormatBool(success),
	}).Observe(runDuration)
}

package main

import (
	"context"
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/go-version"
	hcinstall "github.com/hashicorp/hc-install"
	"github.com/hashicorp/hc-install/fs"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/hc-install/src"
	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/utilitywarehouse/terraform-applier/git"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	terraformapplierv1alpha1 "github.com/utilitywarehouse/terraform-applier/api/v1alpha1"
	"github.com/utilitywarehouse/terraform-applier/controllers"

	//+kubebuilder:scaffold:imports

	"github.com/utilitywarehouse/terraform-applier/log"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/run"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"github.com/utilitywarehouse/terraform-applier/webserver"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(terraformapplierv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

var (
	diffURLFormat      = os.Getenv("DIFF_URL_FORMAT")
	dryRun             = os.Getenv("DRY_RUN")
	fullRunInterval    = os.Getenv("FULL_RUN_INTERVAL_SECONDS")
	listenAddress      = os.Getenv("LISTEN_ADDRESS")
	logLevel           = os.Getenv("LOG_LEVEL")
	pollInterval       = os.Getenv("POLL_INTERVAL_SECONDS")
	modulesPath        = os.Getenv("MODULES_PATH")
	modulesPathFilters = os.Getenv("MODULES_PATH_FILTERS")
	terraformPath      = os.Getenv("TERRAFORM_PATH")
	terraformVersion   = os.Getenv("TERRAFORM_VERSION")
)

func validate() {
	if modulesPath == "" {
		log.Fatal("Need to export MODULES_PATH")
	}

	if listenAddress == "" {
		listenAddress = ":8080"
	}

	if pollInterval == "" {
		pollInterval = "5"
	} else {
		_, err := strconv.Atoi(pollInterval)
		if err != nil {
			log.Fatal("POLL_INTERVAL_SECONDS must be an int")
		}
	}

	if fullRunInterval == "" {
		fullRunInterval = "3600"
	} else {
		_, err := strconv.Atoi(fullRunInterval)
		if err != nil {
			log.Fatal("FULL_RUN_INTERVAL_SECONDS must be an int")
		}
	}

	if diffURLFormat != "" && !strings.Contains(diffURLFormat, "%s") {
		log.Fatal("Invalid DIFF_URL_FORMAT, must contain %q: %v\n", "%s", diffURLFormat)
	}

	if dryRun == "" {
		dryRun = "false"
	} else {
		_, err := strconv.ParseBool(dryRun)
		if err != nil {
			log.Fatal("DRY_RUN must be a boolean")
		}
	}

	if logLevel == "" {
		logLevel = "INFO"
	}
}

// findTerraformExecPath will find the terraform binary to use based on the
// following strategy:
//   - If 'path' is set, try to use that
//   - Otherwise, download the release indicated by 'version'
//   - If the version isn't defined, download the latest release
func findTerraformExecPath(ctx context.Context, path, ver string) (string, func(), error) {
	cleanup := func() {}
	i := hcinstall.NewInstaller()
	var execPath string
	var err error

	if path != "" {
		log.Info("Using terraform version at %s", path)
		execPath, err = i.Ensure(context.Background(), []src.Source{
			&fs.AnyVersion{
				ExactBinPath: path,
			},
		})
	} else if ver != "" {
		tfver := version.Must(version.NewVersion(ver))
		execPath, err = i.Ensure(context.Background(), []src.Source{
			&releases.ExactVersion{
				Product: product.Terraform,
				Version: tfver,
			},
		})
	} else {
		execPath, err = i.Ensure(context.Background(), []src.Source{
			&releases.LatestVersion{
				Product: product.Terraform,
			},
		})
	}

	if err != nil {
		return "", cleanup, err
	}

	return execPath, cleanup, nil
}

// terraformVersionString returns the terraform version from the terraform binary
// indicated by execPath
func terraformVersionString(ctx context.Context, execPath string) (string, error) {
	tmpDir, err := ioutil.TempDir("", "tfversion")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	tf, err := tfexec.NewTerraform(tmpDir, execPath)
	if err != nil {
		return "", err
	}
	version, _, err := tf.Version(context.Background(), true)
	if err != nil {
		return "", err
	}

	return version.String(), nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "4ee367ac.uw.systems",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.WorkspaceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Workspace")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	log.Level = log.LevelFromString(logLevel)

	validate()

	clock := &sysutil.Clock{}

	metrics := &metrics.Prometheus{}
	metrics.Init()

	// Webserver and scheduler send run requests to runQueue channel, runner receives the requests and initiates runs.
	// Only 1 pending request may sit in the queue at a time.
	runQueue := make(chan bool, 1)

	// Runner sends run results to runResults channel, webserver receives the results and displays them.
	// Limit of 5 is arbitrary - there is significant delay between sends, and receives are handled near instantaneously.
	runResults := make(chan run.Result, 5)

	// Runner, webserver, and scheduler all send fatal errors to errors channel, and main() exits upon receiving an error.
	// No limit needed, as a single fatal error will exit the program anyway.
	errors := make(chan error)

	// Find the requested version of terraform and log the version
	// information
	execPath, cleanup, err := findTerraformExecPath(context.Background(), terraformPath, terraformVersion)
	defer cleanup()
	if err != nil {
		log.Fatal("error finding terraform: %s", err)
	}
	version, err := terraformVersionString(context.Background(), execPath)
	if err != nil {
		log.Fatal("error running `terraform version`: %s", err)
	}
	log.Info("Using terraform version: %s", version)

	dr, _ := strconv.ParseBool(dryRun)
	applier := &run.Applier{
		Clock:             clock,
		DryRun:            dr,
		Errors:            errors,
		Metrics:           metrics,
		TerraformExecPath: execPath,
	}

	gitUtil := &git.Util{
		Path: modulesPath,
	}

	var modulesPathFiltersSlice []string
	if modulesPathFilters != "" {
		modulesPathFiltersSlice = strings.Split(modulesPathFilters, ",")
	}
	runner := &run.Runner{
		ModulesPath:        modulesPath,
		ModulesPathFilters: modulesPathFiltersSlice,
		Applier:            applier,
		DiffURLFormat:      diffURLFormat,
		GitUtil:            gitUtil,
		Metrics:            metrics,
		Clock:              clock,
		RunQueue:           runQueue,
		RunResults:         runResults,
		Errors:             errors,
	}

	pi, _ := strconv.Atoi(pollInterval)
	fi, _ := strconv.Atoi(fullRunInterval)
	scheduler := &run.Scheduler{
		FullRunInterval:    time.Duration(fi) * time.Second,
		GitUtil:            gitUtil,
		PollInterval:       time.Duration(pi) * time.Second,
		ModulesPathFilters: modulesPathFiltersSlice,
		RunQueue:           runQueue,
		Errors:             errors,
	}

	webserver := &webserver.WebServer{
		ListenAddress: listenAddress,
		Clock:         clock,
		RunQueue:      runQueue,
		RunResults:    runResults,
		Errors:        errors,
	}
	_ = scheduler
	_ = webserver
	// go scheduler.Start()
	// go runner.Start()
	// go webserver.Start()
	go func() {
		setupLog.Info("starting manager")
		if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			setupLog.Error(err, "problem running manager")
			os.Exit(1)
		}
	}()

	// Wait for apply runs to finish before exiting when a SIGINT or SIGTERM
	// is received. This should prevent state locks being left behind by
	// interrupted terraform commands.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		sig := <-sigCh

		log.Info("Received signal: %s, waiting for apply runs to finish before exiting", sig)

		for {
			select {
			case sig := <-sigCh:
				log.Fatal("Received a second signal: %s, force exiting", sig)
			default:
				if !runner.Applying() {
					os.Exit(0)
				}
			}
		}
	}()

	err = <-errors
	log.Fatal("Fatal error, exiting: %v", err)
}

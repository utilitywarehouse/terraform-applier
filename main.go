package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-version"
	hcinstall "github.com/hashicorp/hc-install"
	"github.com/hashicorp/hc-install/fs"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/hc-install/src"
	"github.com/hashicorp/terraform-exec/tfexec"

	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/runner"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"github.com/utilitywarehouse/terraform-applier/vault"
	"github.com/utilitywarehouse/terraform-applier/webserver"
	"github.com/utilitywarehouse/terraform-applier/webserver/oidc"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	logger            hclog.Logger
	oidcAuthenticator *oidc.Authenticator

	scheme = runtime.NewScheme()

	labelSelectorKey   string
	labelSelectorValue string

	watchNamespaces []string

	terminationGracePeriodDuration time.Duration
	minIntervalBetweenRunsDuration time.Duration
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(tfaplv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	logger = hclog.New(&hclog.LoggerOptions{
		Name:            "terraform-applier",
		Level:           hclog.LevelFromString("warn"),
		IncludeLocation: false,
	})
}

var (
	logLevel                      = getEnv("LOG_LEVEL", "warn")
	repoPath                      = getEnv("REPO_PATH", "/src")
	terminationGracePeriodSeconds = getEnv("TERMINATION_GRACE_PERIOD", "60")
	minIntervalBetweenRuns        = getEnv("MIN_INTERVAL_BETWEEN_RUNS", "60")
	listenAddress                 = getEnv("LISTEN_ADDRESS", ":8080")

	labelSelectorStr   = os.Getenv("CRD_LABEL_SELECTOR")
	watchNamespacesStr = os.Getenv("WATCH_NAMESPACES")
	electionID         = os.Getenv("ELECTION_ID")

	vaultAWSEngPath   = getEnv("VAULT_AWS_ENG_PATH", "/aws")
	vaultKubeAuthPath = getEnv("VAULT_KUBE_AUTH_PATH", "/auth/kubernetes")

	oidcCallbackURL  = os.Getenv("OIDC_CALLBACK_URL")
	oidcClientID     = os.Getenv("OIDC_CLIENT_ID")
	oidcClientSecret = os.Getenv("OIDC_CLIENT_SECRET")
	oidcIssuer       = os.Getenv("OIDC_ISSUER")

	terraformPath    = os.Getenv("TERRAFORM_PATH")
	terraformVersion = os.Getenv("TERRAFORM_VERSION")
)

func getEnv(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if ok {
		return value
	}
	return fallback
}

func validate() {
	logger.SetLevel(hclog.LevelFromString(logLevel))

	tgp, err := strconv.Atoi(terminationGracePeriodSeconds)
	if err != nil {
		logger.Error("TERMINATION_GRACE_PERIOD must be an int")
		os.Exit(1)
	}
	terminationGracePeriodDuration = time.Duration(tgp) * time.Second

	minInterval, err := strconv.Atoi(minIntervalBetweenRuns)
	if err != nil {
		logger.Error("MIN_INTERVAL_BETWEEN_RUNS must be an int")
		os.Exit(1)
	}
	minIntervalBetweenRunsDuration = time.Duration(minInterval) * time.Second

	if labelSelectorStr != "" {
		labelKV := strings.Split(labelSelectorStr, "=")
		if len(labelKV) != 2 || labelKV[0] == "" || labelKV[1] == "" {
			logger.Error("CRD_LABEL_SELECTOR must be in the form 'key=value'")
			os.Exit(1)
		}
		labelSelectorKey = labelKV[0]
		labelSelectorValue = labelKV[1]
	}

	watchNamespaces = strings.Split(watchNamespacesStr, ",")

	// https://github.com/kubernetes-sigs/controller-runtime/issues/2273
	if len(watchNamespaces) > 1 {
		logger.Error("for now only 1 namespace is allowed in watch list WATCH_NAMESPACES")
		os.Exit(1)
	}

	logger.Info("config", "repoPath", repoPath)
	logger.Info("config", "watchNamespaces", watchNamespaces)
	logger.Info("config", "selectorLabel", fmt.Sprintf("%s:%s", labelSelectorKey, labelSelectorValue))
	logger.Info("config", "minIntervalBetweenRunsDuration", minIntervalBetweenRunsDuration)
	logger.Info("config", "terminationGracePeriodDuration", terminationGracePeriodDuration)

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
	tmpDir, err := os.MkdirTemp("", "tfversion")
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

func kubeClient() (*kubernetes.Clientset, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to create in-cluster config err:%s", err)
	}

	// creates the clientset
	return kubernetes.NewForConfig(config)
}

func generateElectionID(salt, labelSelectorKey, labelSelectorValue string, watchNamespaces []string) string {
	h := sha256.New()
	io.WriteString(h,
		fmt.Sprintf("%s-%s-%s-%s", salt, labelSelectorKey, labelSelectorValue, watchNamespaces))
	return fmt.Sprintf("%x.terraform-applier.uw.systems", h.Sum(nil)[:5])
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	validate()

	setupLog := logger.Named("setup")

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8081", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	wsQueue := make(chan runner.Request)
	done := make(chan bool, 1)

	clock := &sysutil.Clock{}

	metrics := &metrics.Prometheus{}
	metrics.Init()

	gitUtil := &git.Util{
		Path: repoPath,
	}

	// Find the requested version of terraform and log the version
	// information
	execPath, cleanup, err := findTerraformExecPath(ctx, terraformPath, terraformVersion)
	defer cleanup()
	if err != nil {
		setupLog.Error("error finding terraform", "err", err)
		os.Exit(1)
	}
	version, err := terraformVersionString(ctx, execPath)
	if err != nil {
		setupLog.Error("error getting terraform version", "err", err)
		os.Exit(1)
	}
	setupLog.Info("found terraform binary", "version", version)

	if electionID == "" {
		electionID = generateElectionID("4ee367ac", labelSelectorKey, labelSelectorValue, watchNamespaces)
	}

	options := ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       electionID,
	}

	var labelSelector labels.Selector
	if labelSelectorKey != "" {
		labelSelector = labels.Set{labelSelectorKey: labelSelectorValue}.AsSelector()
	}

	var watchNS string
	if len(watchNamespaces) > 0 {
		watchNS = watchNamespaces[0]
	}

	options.NewCache = cache.BuilderWithOptions(cache.Options{
		Scheme:    scheme,
		Namespace: watchNS,
		SelectorsByObject: cache.SelectorsByObject{
			&tfaplv1beta1.Module{}: {
				Label: labelSelector,
			},
		},
	})

	filter := &controllers.Filter{
		Log:                logger.Named("filter"),
		LabelSelectorKey:   labelSelectorKey,
		LabelSelectorValue: labelSelectorValue,
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error("unable to start manager", "err", err)
		os.Exit(1)
	}

	if err = (&controllers.ModuleReconciler{
		Client:                 mgr.GetClient(),
		Scheme:                 mgr.GetScheme(),
		Recorder:               mgr.GetEventRecorderFor("terraform-applier"),
		Clock:                  clock,
		Queue:                  wsQueue,
		GitUtil:                gitUtil,
		Log:                    logger.Named("manager"),
		MinIntervalBetweenRuns: minIntervalBetweenRunsDuration,
	}).SetupWithManager(mgr, filter); err != nil {
		setupLog.Error("unable to create module controller", "err", err)
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error("unable to set up health check", "err", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error("unable to set up ready check", "err", err)
		os.Exit(1)
	}

	kubeClient, err := kubeClient()
	if err != nil {
		setupLog.Error("unable to create kube client", "err", err)
		os.Exit(1)
	}
	runner := runner.Runner{
		Clock:                  clock,
		ClusterClt:             mgr.GetClient(),
		Recorder:               mgr.GetEventRecorderFor("terraform-applier"),
		KubeClt:                kubeClient,
		RepoPath:               repoPath,
		Queue:                  wsQueue,
		GitUtil:                gitUtil,
		Log:                    logger.Named("runner"),
		Metrics:                metrics,
		TerraformExecPath:      execPath,
		TerminationGracePeriod: terminationGracePeriodDuration,
		AWSSecretsEngineConfig: &vault.AWSSecretsEngineConfig{
			SecretsEngPath: vaultAWSEngPath,
			AuthPath:       vaultKubeAuthPath,
		},
	}

	// try to setup oidc if any of the oidc ENVs set
	if strings.Join([]string{oidcIssuer, oidcClientID, oidcClientSecret, oidcCallbackURL}, "") != "" {
		oidcAuthenticator, err = oidc.NewAuthenticator(oidcIssuer, oidcClientID, oidcClientSecret, oidcCallbackURL)
		if err != nil {
			setupLog.Error("could not setup oidc authenticator", "error", err)
			os.Exit(1)
		}
		setupLog.Info("OIDC authentication configured", "issuer", oidcIssuer, "clientID", oidcClientID)
	}

	webserver := &webserver.WebServer{
		Authenticator: oidcAuthenticator,
		ListenAddress: listenAddress,
		ClusterClt:    mgr.GetClient(),
		KubeClient:    kubeClient,
		RunQueue:      wsQueue,
		Log:           logger.Named("webserver"),
	}

	go runner.Start(ctx, done)
	go func() {
		err := webserver.Start(ctx)
		if err != nil {
			setupLog.Error("unable to start webserver", "err", err)
		}
	}()

	go func() {
		setupLog.Info("starting manager")
		if err := mgr.Start(ctx); err != nil {
			setupLog.Error("problem running manager", "err", err)
		}
	}()

	// Wait for apply runs to finish before exiting when a SIGINT or SIGTERM
	// is received. This should prevent state locks being left behind by
	// interrupted terraform commands.
	go func(cancel context.CancelFunc) {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		sig := <-sigCh
		setupLog.Info("received Term signal, waiting for all running modules to finish before exiting", "signal", sig)

		// signal runner to start shutting down...
		cancel()

		for {
			select {
			case sig := <-sigCh:
				setupLog.Error("received a second signal, force exiting", "signal", sig)
				os.Exit(1)

			// wait for runner to finish
			case <-done:
				setupLog.Info("runner successfully shutdown")
			}
		}
	}(cancel)

	<-done
}

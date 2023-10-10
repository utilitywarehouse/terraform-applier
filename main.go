package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
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
	"github.com/urfave/cli/v2"
	"go.uber.org/zap/zapcore"

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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	runTimeMetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	logger            hclog.Logger
	oidcAuthenticator *oidc.Authenticator

	scheme = runtime.NewScheme()

	logLevel           string
	reposRootPath      string
	labelSelectorKey   string
	labelSelectorValue string
	electionID         string
	terraformPath      string
	terraformVersion   string
	watchNamespaces    []string
	globalRunEnv       map[string]string

	zapLevelStrings = map[string]zapcore.Level{
		"trace": zapcore.DebugLevel,
		"debug": zapcore.DebugLevel,
		"info":  zapcore.InfoLevel,
		"warn":  zapcore.WarnLevel,
		"error": zapcore.ErrorLevel,
	}

	flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "repos-root-path",
			EnvVars:     []string{"REPOS_ROOT_PATH"},
			Value:       "/src",
			Destination: &reposRootPath,
			Usage: "Absolute path to the directory containing all repositories of the modules. " +
				"The immediate subdirectories of this directory should contain the module repo directories and directory name should match repoName referenced in  module.",
		},
		&cli.StringFlag{
			Name:    "config",
			EnvVars: []string{"TF_APPLIER_CONFIG"},
			Value:   "/config/config.yaml",
			Usage:   "Absolute path to the config file.",
		},
		&cli.IntFlag{
			Name:    "min-interval-between-runs",
			EnvVars: []string{"MIN_INTERVAL_BETWEEN_RUNS"},
			Value:   60,
			Usage:   "The minimum interval in seconds, user can set between 2 consecutive runs. This value defines the frequency of runs.",
		},
		&cli.IntFlag{
			Name:    "termination-grace-period",
			EnvVars: []string{"TERMINATION_GRACE_PERIOD"},
			Value:   60,
			Usage: "Termination grace period in second, is the time given to the running job to finish current run after 1st TERM signal is received. " +
				"After this timeout runner will be forced to shutdown.",
		},
		&cli.StringFlag{
			Name:        "terraform-path",
			EnvVars:     []string{"TERRAFORM_PATH"},
			Destination: &terraformPath,
			Usage:       "The local path to a terraform binary to use.",
		},
		&cli.StringFlag{
			Name:        "terraform-version",
			EnvVars:     []string{"TERRAFORM_VERSION"},
			Destination: &terraformVersion,
			Usage: "The version of terraform to use. The controller will install the requested release when it starts up. " +
				"if not set, it will choose the latest available one. Ignored if `TERRAFORM_PATH` is set.",
		},
		&cli.BoolFlag{
			Name:    "set-git-ssh-command-global-env",
			EnvVars: []string{"SET_GIT_SSH_COMMAND_GLOBAL_ENV"},
			Value:   false,
			Usage: "If set GIT_SSH_COMMAND env will be set as global env for all modules. " +
				"This ssh command will be used by modules during terraform init to pull private remote modules.",
		},
		&cli.StringFlag{
			Name:    "git-ssh-key-file",
			EnvVars: []string{"GIT_SSH_KEY_FILE"},
			Value:   "/etc/git-secret/ssh",
			Usage:   "The path to git ssh key which will be used to setup GIT_SSH_COMMAND env.",
		},
		&cli.StringFlag{
			Name:    "git-ssh-known-hosts-file",
			EnvVars: []string{"GIT_SSH_KNOWN_HOSTS_FILE"},
			Value:   "/etc/git-secret/known_hosts",
			Usage:   "The local path to the known hosts file used to setup GIT_SSH_COMMAND env.",
		},
		&cli.BoolFlag{
			Name:    "git-verify-known-hosts",
			EnvVars: []string{"GIT_VERIFY_KNOWN_HOSTS"},
			Value:   true,
			Usage:   "The local path to the known hosts file used to setup GIT_SSH_COMMAND env.",
		},
		&cli.StringFlag{
			Name:    "controller-runtime-env",
			EnvVars: []string{"CONTROLLER_RUNTIME_ENV"},
			Usage: "The comma separated list of ENVs which will be passed from controller to its managed modules during terraform run. " +
				"The values should be set on the controller.",
		},
		&cli.BoolFlag{
			Name:  "cleanup-temp-dir",
			Value: false,
			Usage: "If set, the OS temporary directory will be removed and re-created. This can help removing redundant terraform" +
				"binaries and avoiding temp directory growing in size with every restart.",
		},
		&cli.StringFlag{
			Name:    "module-label-selector",
			EnvVars: []string{"MODULE_LABEL_SELECTOR"},
			Usage: "If set controller will only watch and process modules with this label. " +
				"value should be in the form of 'label-key=label-value'.",
		},
		&cli.StringFlag{
			Name:    "watch-namespaces",
			EnvVars: []string{"WATCH_NAMESPACES"},
			Usage: "if set controller will only watch given namespaces for modules. " +
				"it will operate in namespace scope mode.",
		},
		&cli.BoolFlag{
			Name:    "leader-elect",
			EnvVars: []string{"LEADER_ELECT"},
			Value:   false,
			Usage: "Enable leader election for controller manager. " +
				"Enabling this will ensure there is only one active controller manager.",
		},
		&cli.StringFlag{
			Name:        "election-id",
			EnvVars:     []string{"ELECTION_ID"},
			Destination: &electionID,
			Usage: "it determines the name of the resource that leader election will use for holding the leader lock. " +
				"if multiple controllers are running with same label selector and watch namespace value then they belong to same stack. if election enabled, " +
				"election id needs to be unique per stack. If this is not unique to the stack then only one stack will be working concurrently. " +
				"if not set value will be auto generated based on given label selector and watch namespace value.",
		},

		&cli.StringFlag{
			Name:    "vault-aws-secret-engine-path",
			EnvVars: []string{"VAULT_AWS_SEC_ENG_PATH"},
			Value:   "/aws",
			Usage:   "The path where AWS secrets engine is enabled.",
		},
		&cli.StringFlag{
			Name:    "vault-kube-auth-path",
			EnvVars: []string{"VAULT_KUBE_AUTH_PATH"},
			Value:   "/auth/kubernetes",
			Usage:   "The path where kubernetes auth method is mounted.",
		},

		&cli.StringFlag{
			Name:    "oidc-issuer",
			EnvVars: []string{"OIDC_ISSUER"},
			Usage:   "The url of the IDP where OIDC app is created.",
		},
		&cli.StringFlag{
			Name:    "oidc-client-id",
			EnvVars: []string{"OIDC_CLIENT_ID"},
			Usage:   "The client ID of the OIDC app.",
		},
		&cli.StringFlag{
			Name:    "oidc-client-secret",
			EnvVars: []string{"OIDC_CLIENT_SECRET"},
			Usage:   "The client secret of the OIDC app.",
		},
		&cli.StringFlag{
			Name:    "oidc-callback-url",
			EnvVars: []string{"OIDC_CALLBACK_URL"},
			Usage:   "The callback url used for OIDC auth flow, this should be the terraform-applier url.",
		},

		&cli.StringFlag{
			Name:        "log-level",
			EnvVars:     []string{"LOG_LEVEL"},
			Value:       "info",
			Destination: &logLevel,
			Usage:       "Log level",
		},
		&cli.StringFlag{
			Name:  "webserver-bind-address",
			Value: ":8080",
			Usage: "The address the web server binds to.",
		},
		&cli.StringFlag{
			Name:  "metrics-bind-address",
			Value: ":8081",
			Usage: "The address the metric endpoint binds to.",
		},
		&cli.StringFlag{
			Name:  "health-probe-bind-address",
			Value: ":8082",
			Usage: "The address the probe endpoint binds to.",
		},
	}
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(tfaplv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	logger = hclog.New(&hclog.LoggerOptions{
		Name:            "terraform-applier",
		Level:           hclog.Info,
		IncludeLocation: false,
	})
}

func validate(c *cli.Context) {
	logger.SetLevel(hclog.LevelFromString(logLevel))

	if c.IsSet("module-label-selector") {
		labelKV := strings.Split(c.String("module-label-selector"), "=")
		if len(labelKV) != 2 || labelKV[0] == "" || labelKV[1] == "" {
			logger.Error("CRD_LABEL_SELECTOR must be in the form 'key=value'")
			os.Exit(1)
		}
		labelSelectorKey = labelKV[0]
		labelSelectorValue = labelKV[1]
	}

	if c.IsSet("watch-namespaces") {
		watchNamespaces = strings.Split(c.String("watch-namespaces"), ",")
	}

	logger.Info("config", "reposRootPath", reposRootPath)
	logger.Info("config", "watchNamespaces", watchNamespaces)
	logger.Info("config", "selectorLabel", fmt.Sprintf("%s:%s", labelSelectorKey, labelSelectorValue))
	logger.Info("config", "minIntervalBetweenRunsDuration", c.Int("min-interval-between-runs"))
	logger.Info("config", "terminationGracePeriodDuration", c.Int("termination-grace-period"))

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

func setupGlobalEnv(c *cli.Context) {

	globalRunEnv = make(map[string]string)

	// terraform depends on git for pulling remote modules
	globalRunEnv["PATH"] = os.Getenv("PATH")

	for _, env := range strings.Split(c.String("controller-runtime-env"), ",") {
		globalRunEnv[env] = os.Getenv(env)
	}

	if c.Bool("set-git-ssh-command-global-env") {
		cmdStr, err := git.GitSSHCommand(
			c.String("git-ssh-key-file"), c.String("git-ssh-known-hosts-file"), c.Bool("git-verify-known-hosts"),
		)
		if err != nil {
			logger.Error("unable to set GIT_SSH_COMMAND", "err", err)
			os.Exit(1)
		}
		logger.Info("setting GIT_SSH_COMMAND as global env", "value", cmdStr)
		globalRunEnv["GIT_SSH_COMMAND"] = cmdStr
	}

	// KUBE_CONFIG_PATH will be used by modules with kubernetes backend.
	// ideally modules should be using own SA to auth with kube cluster and not depend on default in cluster config of controller's SA
	// but without this config terraform ignores `host` and `token` backend attributes
	// hence generating config with just server info and setting it as Global ENV
	// https://github.com/hashicorp/terraform/issues/31275
	configPath := filepath.Join(os.TempDir(), "tf-applier-in-cluster-config")
	globalRunEnv["KUBE_CONFIG_PATH"] = configPath

	defaultCurrentKubeConfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
    server: https://kubernetes.default.svc
  name: in-cluster
contexts:
- context:
    cluster: in-cluster
  name: in-cluster
current-context: in-cluster
users: []
preferences: {}
`
	err := os.WriteFile(configPath, []byte(defaultCurrentKubeConfig), 0666)
	if err != nil {
		logger.Error("unable to create custom in cluster config file", "err", err)
		os.Exit(1)
	}

}

func cleanupTmpDir() {
	tmpDir := os.TempDir()

	tmpDirCleanupCommand := fmt.Sprintf("rm -rf %s/* %s/*", tmpDir, reposRootPath)

	cmd := exec.Command("sh", "-c", tmpDirCleanupCommand)
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

func main() {
	app := &cli.App{
		Name: "terraform-applier",
		Usage: "terraform-applier is a kube controller for module CRD. " +
			"module enables continuous deployment of Terraform modules by applying from a Git repository.",
		Flags: flags,
		Action: func(cCtx *cli.Context) error {
			validate(cCtx)
			// Cleanup temp directory if the corresponding flag is set
			if cCtx.Bool("cleanup-temp-dir") {
				cleanupTmpDir()
			}
			setupGlobalEnv(cCtx)
			run(cCtx)
			return nil
		},
	}

	app.Run(os.Args)
}

func run(c *cli.Context) {
	ctx, cancel := context.WithCancel(c.Context)

	setupLog := logger.Named("setup")

	opts := zap.Options{
		Development: true,
		Level:       zapLevelStrings[strings.ToLower(logLevel)],
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// runStatus keeps track of currently running modules
	runStatus := new(sync.Map)

	moduleQueue := make(chan runner.Request)
	done := make(chan bool, 1)

	clock := &sysutil.Clock{}

	metrics := &metrics.Prometheus{}
	metrics.Init()

	conf, err := parseConfigFile(c.String("config"))
	if err != nil {
		setupLog.Error("unable to parse tf applier config file", "err", err)
		os.Exit(1)
	}

	git.EnableMetrics("terraform_applier", runTimeMetrics.Registry)

	gitSyncPool, err := git.NewSyncPool(
		ctx,
		reposRootPath,
		git.SyncOptions{
			GitSSHKeyPath:        c.String("git-ssh-key-file"),
			GitSSHKnownHostsPath: c.String("git-ssh-known-hosts-file"),
			Interval:             30 * time.Second,
			WithCheckout:         true,
		},
		logger.Named("git-sync"),
	)
	if err != nil {
		setupLog.Error("could not create git sync pool", "err", err)
		os.Exit(1)
	}

	for name, repoConfig := range conf.Repositories {
		err := gitSyncPool.AddRepository(name, repoConfig, nil)
		if err != nil {
			setupLog.Error("unable to add repository to git sync pool", "name", name, "err", err)
			os.Exit(1)
		}
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
		Metrics:                metricsserver.Options{BindAddress: c.String("metrics-bind-address")},
		HealthProbeBindAddress: c.String("health-probe-bind-address"),
		LeaderElection:         c.Bool("leader-elect"),
		LeaderElectionID:       electionID,
	}

	var labelSelector labels.Selector
	if labelSelectorKey != "" {
		labelSelector = labels.Set{labelSelectorKey: labelSelectorValue}.AsSelector()
	}

	// if watchNamespaces specified only watch/cache modules form those namespaces
	var namespaces map[string]cache.Config
	if len(watchNamespaces) > 0 {
		namespaces = make(map[string]cache.Config)
		for _, wn := range watchNamespaces {
			namespaces[wn] = cache.Config{}
		}
	}

	options.Cache = cache.Options{
		Scheme:               scheme,
		DefaultLabelSelector: labelSelector,
		ByObject: map[client.Object]cache.ByObject{
			&tfaplv1beta1.Module{}: {Namespaces: namespaces},
		},
	}

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
		Queue:                  moduleQueue,
		GitSyncPool:            gitSyncPool,
		Log:                    logger.Named("manager"),
		MinIntervalBetweenRuns: time.Duration(c.Int("min-interval-between-runs")) * time.Second,
		RunStatus:              runStatus,
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
		Queue:                  moduleQueue,
		GitSyncPool:            gitSyncPool,
		Log:                    logger.Named("runner"),
		Metrics:                metrics,
		TerraformExecPath:      execPath,
		TerminationGracePeriod: time.Duration(c.Int("termination-grace-period")) * time.Second,
		AWSSecretsEngineConfig: &vault.AWSSecretsEngineConfig{
			SecretsEngPath: c.String("vault-aws-secret-engine-path"),
			AuthPath:       c.String("vault-kube-auth-path"),
		},
		GlobalENV: globalRunEnv,
		RunStatus: runStatus,
	}

	if c.IsSet("oidc-issuer") {
		oidcAuthenticator, err = oidc.NewAuthenticator(
			c.String("oidc-issuer"),
			c.String("oidc-client-id"),
			c.String("oidc-client-secret"),
			c.String("oidc-callback-url"),
		)
		if err != nil {
			setupLog.Error("could not setup oidc authenticator", "error", err)
			os.Exit(1)
		}
		setupLog.Info("OIDC authentication configured", "issuer", c.String("oidc-issuer"), "clientID", c.String("oidc-client-id"))
	}

	webserver := &webserver.WebServer{
		Authenticator: oidcAuthenticator,
		ListenAddress: c.String("webserver-bind-address"),
		ClusterClt:    mgr.GetClient(),
		KubeClient:    kubeClient,
		RunQueue:      moduleQueue,
		RunStatus:     runStatus,
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

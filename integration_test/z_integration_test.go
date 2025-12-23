package integration_test

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/hc-install/src"
	ctrl "sigs.k8s.io/controller-runtime"

	hcinstall "github.com/hashicorp/hc-install"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/controllers"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/runner"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"github.com/utilitywarehouse/terraform-applier/vault"
)

const (
	TestENVK8sVersion = "1.33.0"
	TestLogLevel      = slog.LevelInfo //can be slog.LevelDebug or slog.Level(-8)
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc

	fakeClock  *sysutil.FakeClock
	testLogger *slog.Logger

	testStateFilePath string
	testFilter        *controllers.Filter

	testRepos        *git.MockRepositories
	testMetrics      *metrics.MockPrometheusInterface
	testDelegate     *runner.MockDelegateInterface
	testRedis        *sysutil.MockRedisInterface
	testRunner       runner.Runner
	testMockRunner1  *runner.MockRunnerInterface //only used for controller behaviour testing without runner
	testMockRunner2  *runner.MockRunnerInterface //only used for controller behaviour testing without runner
	testReconciler   *controllers.ModuleReconciler
	testVaultAWSConf *vault.MockProviderInterface
	testCreds        *sysutil.MockCredsProvider
)

func TestMain(m *testing.M) {
	testLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: TestLogLevel,
	}))

	var err error
	// Download test assets to ./bin dir
	k8sAssetPath, err := exec.Command(
		"go", "run",
		"sigs.k8s.io/controller-runtime/tools/setup-envtest",
		"use", TestENVK8sVersion, "--bin-dir", "../bin", "-p", "path",
	).Output()

	if err != nil {
		testLogger.Error("Failed to setup envtest", "error", err)
		os.Exit(1)
	}

	if err := os.Setenv("KUBEBUILDER_ASSETS", string(k8sAssetPath)); err != nil {
		testLogger.Error("Failed to set KUBEBUILDER_ASSETS", "error", err)
		os.Exit(1)
	}

	logf.SetLogger(logr.FromSlogHandler(testLogger.Handler()))

	ctx, cancel = context.WithCancel(context.TODO())

	testLogger.Info("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err = testEnv.Start()
	if err != nil {
		testLogger.Error("Failed to start testEnv", "error", err)
		os.Exit(1)
	}
	if cfg == nil {
		testLogger.Error("Config is nil")
		os.Exit(1)
	}

	if err = tfaplv1beta1.AddToScheme(scheme.Scheme); err != nil {
		testLogger.Error("Failed to add scheme", "error", err)
		os.Exit(1)
	}

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		testLogger.Error("Failed to create k8sClient", "error", err)
		os.Exit(1)
	}

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Controller: config.Controller{
			MaxConcurrentReconciles: 10,
		},
	})
	if err != nil {
		testLogger.Error("Failed to create manager", "error", err)
		os.Exit(1)
	}

	fakeClock = &sysutil.FakeClock{
		T: time.Date(01, 01, 01, 0, 0, 0, 0, time.UTC),
	}

	runStatus := sysutil.NewRunStatus()
	minIntervalBetweenRunsDuration := 1 * time.Minute

	// Initial placeholder initialization for globals.
	// Actual mocks are re-created per test in setupTest(t)
	// but we need the struct pointers to remain consistent for the Manager.

	// We initialize the Reconciler struct once.
	// The fields (interfaces) will be updated by setupTest() before each test.
	testReconciler = &controllers.ModuleReconciler{
		Client:                 k8sManager.GetClient(),
		Scheme:                 k8sManager.GetScheme(),
		Recorder:               k8sManager.GetEventRecorderFor("terraform-applier"),
		Clock:                  fakeClock,
		Log:                    testLogger.With("logger", "manager"),
		MinIntervalBetweenRuns: minIntervalBetweenRunsDuration,
		RunStatus:              runStatus,
	}

	testFilter = &controllers.Filter{
		Log:                testLogger.With("logger", "filter"),
		LabelSelectorKey:   "",
		LabelSelectorValue: "",
	}

	if err = testReconciler.SetupWithManager(k8sManager, testFilter); err != nil {
		testLogger.Error("Failed to setup reconciler with manager", "error", err)
		os.Exit(1)
	}

	go func() {
		if err = k8sManager.Start(ctx); err != nil {
			testLogger.Error("Manager failed", "error", err)
			os.Exit(1)
		}
	}()

	// Setup Runner
	fakeClient := fake.NewSimpleClientset()
	execPath, err := hcinstall.NewInstaller().Ensure(context.Background(), []src.Source{
		&releases.LatestVersion{
			Product: product.Terraform,
		},
	})
	if err != nil {
		testLogger.Error("Failed to setup TF bin", "error", err)
		os.Exit(1)
	}

	testRunner = runner.Runner{
		Clock:                  fakeClock,
		ClusterClt:             k8sManager.GetClient(),
		Recorder:               k8sManager.GetEventRecorderFor("terraform-applier"),
		KubeClt:                fakeClient,
		Log:                    testLogger.With("logger", "runner"),
		TerraformExecPath:      execPath,
		TerminationGracePeriod: 10 * time.Second,
		RunStatus:              runStatus,
		DataRootPath:           os.TempDir(),
	}

	if err = testRunner.Init(false, 10); err != nil {
		testLogger.Error("Failed to init runner", "error", err)
		os.Exit(1)
	}

	pwd, err := os.Getwd()
	if err != nil {
		testLogger.Error("Failed to get wd", "error", err)
		os.Exit(1)
	}
	testStateFilePath = filepath.Join(pwd, "src", "modules", "hello", "terraform.tfstate")

	// Run Tests
	exitCode := m.Run()

	// Teardown
	cancel()
	testLogger.Info("tearing down the test environment")
	if err := testEnv.Stop(); err != nil {
		testLogger.Error("Failed to stop testEnv", "error", err)
	}
	os.Remove(testStateFilePath)

	os.Exit(exitCode)
}

// setupTest initializes mocks for a specific test execution and updates the global references.
// It returns a gomock Controller that must be finished by the caller.
func setupTest(t *testing.T) *gomock.Controller {
	ctrl := gomock.NewController(t)

	// Initialize Mocks
	testRepos = git.NewMockRepositories(ctrl)
	testMetrics = metrics.NewMockPrometheusInterface(ctrl)
	testDelegate = runner.NewMockDelegateInterface(ctrl)
	testMockRunner1 = runner.NewMockRunnerInterface(ctrl)
	testMockRunner2 = runner.NewMockRunnerInterface(ctrl)
	testRedis = sysutil.NewMockRedisInterface(ctrl)
	testCreds = sysutil.NewMockCredsProvider(ctrl)
	testVaultAWSConf = vault.NewMockProviderInterface(ctrl)

	// Update Reconciler with new mocks
	testReconciler.Repos = testRepos
	testReconciler.Metrics = testMetrics
	// Note: Runner is swapped in specific tests

	// Update Runner with new mocks
	testRunner.Repos = testRepos
	testRunner.Metrics = testMetrics
	testRunner.Delegate = testDelegate
	testRunner.Vault = testVaultAWSConf
	testRunner.Redis = testRedis
	testRunner.GHCredsProvider = testCreds

	return ctrl
}

// triggerReconcile touches the module in a way that forces the controller to reconcile immediately.
// We update the Spec (specifically PollInterval) to trigger a Generation change.
// This is required because the Controller's Predicate filters out updates that don't change
// the Generation or the RunRequest annotation.
func triggerReconcile(ctx context.Context, clt client.Client, key types.NamespacedName) error {
	module := &tfaplv1beta1.Module{}
	if err := clt.Get(ctx, key, module); err != nil {
		return err
	}

	module.Spec.PollInterval++

	return clt.Update(ctx, module)
}

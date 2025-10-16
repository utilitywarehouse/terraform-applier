/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	ctrl "sigs.k8s.io/controller-runtime"

	hcinstall "github.com/hashicorp/hc-install"
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
	//+kubebuilder:scaffold:imports
)

const (
	TestENVK8sVersion = "1.33.0"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc

	fakeClock  *sysutil.FakeClock
	goMockCtrl *gomock.Controller
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

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	// fetch the current config
	suiteConfig, reporterConfig := GinkgoConfiguration()
	// adjust it
	suiteConfig.SkipStrings = []string{"NEVER-RUN"}
	reporterConfig.Verbose = true
	reporterConfig.FullTrace = true
	// pass it in to RunSpecs
	RunSpecs(t, "Controller Suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	testLogger = slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{
		Level: slog.Level(-8),
	}))

	var err error
	// Download test assets to ./bin dir
	k8sAssetPath, err := exec.Command(
		"go", "run",
		"sigs.k8s.io/controller-runtime/tools/setup-envtest",
		"use", TestENVK8sVersion, "--bin-dir", "../bin", "-p", "path",
	).Output()

	Expect(err).NotTo(HaveOccurred())

	Expect(os.Setenv("KUBEBUILDER_ASSETS", string(k8sAssetPath))).To(Succeed())

	logf.SetLogger(logr.FromSlogHandler(testLogger.Handler()))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = tfaplv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Controller: config.Controller{
			// always test with concurrency
			MaxConcurrentReconciles: 10,
		},
	})
	Expect(err).ToNot(HaveOccurred())

	fakeClock = &sysutil.FakeClock{
		T: time.Date(01, 01, 01, 0, 0, 0, 0, time.UTC),
	}

	runStatus := sysutil.NewRunStatus()

	minIntervalBetweenRunsDuration := 1 * time.Minute

	goMockCtrl = gomock.NewController(RecoveringGinkgoT())

	testRepos = git.NewMockRepositories(goMockCtrl)
	testMetrics = metrics.NewMockPrometheusInterface(goMockCtrl)
	testDelegate = runner.NewMockDelegateInterface(goMockCtrl)
	testMockRunner1 = runner.NewMockRunnerInterface(goMockCtrl)
	testMockRunner2 = runner.NewMockRunnerInterface(goMockCtrl)
	testRedis = sysutil.NewMockRedisInterface(goMockCtrl)
	testCreds = sysutil.NewMockCredsProvider(goMockCtrl)

	testVaultAWSConf = vault.NewMockProviderInterface(goMockCtrl)

	testReconciler = &controllers.ModuleReconciler{
		Client:                 k8sManager.GetClient(),
		Scheme:                 k8sManager.GetScheme(),
		Recorder:               k8sManager.GetEventRecorderFor("terraform-applier"),
		Clock:                  fakeClock,
		Repos:                  testRepos,
		Log:                    testLogger.With("logger", "manager"),
		MinIntervalBetweenRuns: minIntervalBetweenRunsDuration,
		RunStatus:              runStatus,
		Metrics:                testMetrics,
	}

	testFilter = &controllers.Filter{
		Log:                testLogger.With("logger", "filter"),
		LabelSelectorKey:   "",
		LabelSelectorValue: "",
	}

	err = testReconciler.SetupWithManager(k8sManager, testFilter)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	// Setup Runner
	fakeClient := fake.NewSimpleClientset()

	execPath, err := setupTFBin()
	Expect(err).NotTo(HaveOccurred())

	testRunner = runner.Runner{
		Clock:                  fakeClock,
		ClusterClt:             k8sManager.GetClient(),
		Recorder:               k8sManager.GetEventRecorderFor("terraform-applier"),
		KubeClt:                fakeClient,
		Repos:                  testRepos,
		GHCredsProvider:        testCreds,
		Delegate:               testDelegate,
		Log:                    testLogger.With("logger", "runner"),
		Metrics:                testMetrics,
		TerraformExecPath:      execPath,
		Vault:                  testVaultAWSConf,
		TerminationGracePeriod: 10 * time.Second,
		RunStatus:              runStatus,
		Redis:                  testRedis,
		DataRootPath:           os.TempDir(),
	}

	err = testRunner.Init(false, 10)
	Expect(err).NotTo(HaveOccurred())

	pwd, err := os.Getwd()
	Expect(err).NotTo(HaveOccurred())
	testStateFilePath = filepath.Join(pwd, "src", "modules", "hello", "terraform.tfstate")

	go func() {
		defer GinkgoRecover()
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())

	os.Remove(testStateFilePath)
})

func setupTFBin() (string, error) {
	execPath, err := hcinstall.NewInstaller().Ensure(context.Background(), []src.Source{
		&releases.LatestVersion{
			Product: product.Terraform,
		},
	})

	return execPath, err
}

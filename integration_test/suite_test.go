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
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/hc-install/src"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	ctrl "sigs.k8s.io/controller-runtime"

	hcinstall "github.com/hashicorp/hc-install"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/controllers"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/runner"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	//+kubebuilder:scaffold:imports
)

const (
	TestENVK8sVersion = "1.26.0"
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
	testLogger hclog.Logger
	// testControllerQueue only used for controller behaviour testing
	testControllerQueue chan reconcile.Request

	// testRunnerQueue only used for send job to runner of runner testing
	testRunnerQueue chan reconcile.Request
	testGitUtil     *git.MockUtilInterface
	testMetrics     *metrics.MockPrometheusInterface
	testDelegate    *runner.MockDelegateInterface
	testRunner      runner.Runner
	testReconciler  *controllers.ModuleReconciler
	testRunnerDone  chan bool
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	var err error
	// Download test assets to ./bin dir
	k8sAssetPath, err := exec.Command(
		"go", "run",
		"sigs.k8s.io/controller-runtime/tools/setup-envtest",
		"use", TestENVK8sVersion, "--bin-dir", "../bin", "-p", "path",
	).Output()
	Expect(err).NotTo(HaveOccurred())

	Expect(os.Setenv("KUBEBUILDER_ASSETS", string(k8sAssetPath))).To(Succeed())

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

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
	})
	Expect(err).ToNot(HaveOccurred())

	testLogger = hclog.New(&hclog.LoggerOptions{
		Name:            "test",
		Level:           hclog.LevelFromString("DEBUG"),
		IncludeLocation: false,
	})

	fakeClock = &sysutil.FakeClock{
		T: time.Date(01, 01, 01, 0, 0, 0, 0, time.UTC),
	}

	minIntervalBetweenRunsDuration := 1 * time.Minute
	testControllerQueue = make(chan ctrl.Request)
	testRunnerQueue = make(chan ctrl.Request)

	goMockCtrl = gomock.NewController(RecoveringGinkgoT())

	testGitUtil = git.NewMockUtilInterface(goMockCtrl)
	testMetrics = metrics.NewMockPrometheusInterface(goMockCtrl)
	testDelegate = runner.NewMockDelegateInterface(goMockCtrl)

	testReconciler = &controllers.ModuleReconciler{
		Client:                 k8sManager.GetClient(),
		Scheme:                 k8sManager.GetScheme(),
		Clock:                  fakeClock,
		Queue:                  testControllerQueue,
		GitUtil:                testGitUtil,
		Log:                    testLogger.Named("manager"),
		MinIntervalBetweenRuns: minIntervalBetweenRunsDuration,
	}

	err = testReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	// Setup Runner
	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "terraform-applier-delegate-token",
				Namespace: "default",
			},
			Type: corev1.SecretTypeServiceAccountToken,
			Data: map[string][]byte{
				"token": []byte("xxxxxxxxxxxxxxxxxxx"),
			},
		},
	)

	execPath, err := setupTFBin()
	Expect(err).NotTo(HaveOccurred())

	testRunnerDone = make(chan bool, 1)
	testRunner = runner.Runner{
		Clock:                  fakeClock,
		ClusterClt:             k8sManager.GetClient(),
		KubeClt:                fakeClient,
		RepoPath:               "modules",
		Queue:                  testRunnerQueue,
		GitUtil:                testGitUtil,
		Delegate:               testDelegate,
		Log:                    testLogger.Named("runner"),
		Metrics:                testMetrics,
		TerraformExecPath:      execPath,
		TerminationGracePeriod: 10 * time.Second,
	}

	go func() {
		defer GinkgoRecover()
		testRunner.Start(ctx, testRunnerDone)
	}()
})

var _ = AfterSuite(func() {
	cancel()
	<-testRunnerDone
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func setupTFBin() (string, error) {
	execPath, err := hcinstall.NewInstaller().Ensure(context.Background(), []src.Source{
		&releases.LatestVersion{
			Product: product.Terraform,
		},
	})

	return execPath, err
}
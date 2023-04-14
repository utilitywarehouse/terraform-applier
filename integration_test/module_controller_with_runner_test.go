package integration_test

import (
	"context"
	"os"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/vault"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("Module controller with Runner", func() {

	const (
		moduleNamespace = "default"
		interval        = time.Millisecond * 250
		commitHash      = "a1b2c3d4"
		commitMsg       = "test commit"
	)

	var (
		boolTrue = true
	)

	Context("When creating Module", func() {
		BeforeEach(func() {
			// reset Time
			fakeClock.T = time.Date(2022, 02, 01, 01, 00, 00, 0000, time.UTC)
			testReconciler.Queue = testRunnerQueue

			// remove any label selector
			testFilter.LabelSelectorKey = ""
			testFilter.LabelSelectorValue = ""

			// Trigger Job run as soon as module is created
			testGitUtil.EXPECT().HeadCommitHashAndLog("hello").
				Return(commitHash, commitMsg, nil).AnyTimes()
			testGitUtil.EXPECT().RemoteURL().Return("github.com/org/repo", nil).AnyTimes()

			testMetrics.EXPECT().UpdateModuleRunDuration(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			testMetrics.EXPECT().UpdateModuleSuccess(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			testMetrics.EXPECT().UpdateTerraformExitCodeCount(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			testMetrics.EXPECT().IncRunningModuleCount(gomock.Any()).AnyTimes()
			testMetrics.EXPECT().DecRunningModuleCount(gomock.Any()).AnyTimes()

			// clear state file if exits
			os.Remove(testStateFilePath)
		})

		It("Should send module to job queue on commit change and runner should do plan & apply", func() {
			const (
				moduleName = "hello"
				path       = "hello"
			)

			By("By creating a new Module")
			ctx := context.Background()
			module := &tfaplv1beta1.Module{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "terraform-applier.uw.systems/v1beta1",
					Kind:       "Module",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: moduleNamespace,
				},
				Spec: tfaplv1beta1.ModuleSpec{
					Schedule: "50 * * * *",
					Path:     path,
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			// Setup FakeDelegation
			fakeClient := fake.NewSimpleClientset()
			testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), gomock.Any()).Return("token.X1", nil)
			testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X1").Return(fakeClient, nil)

			By("By making sure job was sent to jobQueue when commit hash is changed")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Running"))
			Expect(fetchedModule.Status.RunStartedAt.UTC()).Should(Equal(fakeClock.T.UTC()))
			Expect(fetchedModule.Status.RunDuration).Should(BeNil())
			Expect(fetchedModule.Status.ObservedGeneration).Should(Equal(fetchedModule.ObjectMeta.Generation))
			Expect(fetchedModule.Status.RunCommitHash).Should(Equal(commitHash))
			Expect(fetchedModule.Status.RunCommitMsg).Should(Equal(commitMsg))

			// advance time for testing
			fakeClock.T = time.Date(2022, 02, 01, 01, 01, 00, 0000, time.UTC)

			By("By making sure job run finished")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("Running")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Ready"))
			Expect(fetchedModule.Status.RunDuration.Duration.String()).Should(Equal("1m0s"))
			Expect(fetchedModule.Status.StateMessage).Should(ContainSubstring("Apply complete"))

			// Make sure LastDriftInfo & LastApplyInfo is also set
			Expect(fetchedModule.Status.LastDriftInfo.CommitHash).Should(Equal(commitHash))
			Expect(fetchedModule.Status.LastDriftInfo.Output).Should(ContainSubstring("Plan:"))
			Expect(fetchedModule.Status.LastDriftInfo.Timestamp.UTC()).Should(Equal(fakeClock.T.UTC()))

			Expect(fetchedModule.Status.LastApplyInfo.CommitHash).Should(Equal(commitHash))
			Expect(fetchedModule.Status.LastApplyInfo.Output).Should(ContainSubstring("Apply complete!"))
			Expect(fetchedModule.Status.LastApplyInfo.Timestamp.UTC()).Should(Equal(fakeClock.T.UTC()))

		})

		It("Should send module to job queue on commit change and runner should only do plan", func() {
			const (
				moduleName = "hello-plan-only"
				path       = "hello"
			)

			By("By creating a new Module")
			ctx := context.Background()
			module := &tfaplv1beta1.Module{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "terraform-applier.uw.systems/v1beta1",
					Kind:       "Module",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: moduleNamespace,
				},
				Spec: tfaplv1beta1.ModuleSpec{
					Schedule: "50 * * * *",
					Path:     path,
					PlanOnly: &boolTrue,
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			// Setup FakeDelegation
			fakeClient := fake.NewSimpleClientset()
			testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), gomock.Any()).Return("token.X2", nil)
			testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X2").Return(fakeClient, nil)

			By("By making sure job was sent to jobQueue when commit hash is changed")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Running"))
			Expect(fetchedModule.Status.RunStartedAt.UTC()).Should(Equal(fakeClock.T.UTC()))
			Expect(fetchedModule.Status.RunDuration).Should(BeNil())
			Expect(fetchedModule.Status.ObservedGeneration).Should(Equal(fetchedModule.ObjectMeta.Generation))
			Expect(fetchedModule.Status.RunCommitHash).Should(Equal(commitHash))
			Expect(fetchedModule.Status.RunCommitMsg).Should(Equal(commitMsg))

			// advance time for testing
			fakeClock.T = time.Date(2022, 02, 01, 01, 01, 00, 0000, time.UTC)

			By("By making sure job run finished without running plan")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("Running")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Ready"))
			Expect(fetchedModule.Status.RunDuration.Duration.String()).Should(Equal("1m0s"))
			Expect(fetchedModule.Status.StateMessage).Should(ContainSubstring("PlanOnly"))

			// Make sure LastDriftInfo & LastApplyInfo is also set
			Expect(fetchedModule.Status.LastDriftInfo.CommitHash).Should(Equal(commitHash))
			Expect(fetchedModule.Status.LastDriftInfo.Output).Should(ContainSubstring("Plan:"))
			Expect(fetchedModule.Status.LastDriftInfo.Timestamp.UTC()).Should(Equal(fakeClock.T.UTC()))

			Expect(fetchedModule.Status.LastApplyInfo.CommitHash).Should(Equal(""))
			Expect(fetchedModule.Status.LastApplyInfo.Output).Should(ContainSubstring(""))
			Expect(fetchedModule.Status.LastApplyInfo.Timestamp).Should(BeNil())
		})

		It("Should send module to job queue on commit change and runner should read configmaps and secrets before apply and setup local backend", func() {
			const (
				moduleName = "hello-with-var-env"
				path       = "hello"
			)

			By("By creating a new Module")
			ctx := context.Background()
			module := &tfaplv1beta1.Module{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "terraform-applier.uw.systems/v1beta1",
					Kind:       "Module",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: moduleNamespace,
				},
				Spec: tfaplv1beta1.ModuleSpec{
					Schedule: "50 * * * *",
					Path:     path,
					Backend: []corev1.EnvVar{
						{Name: "path", Value: testStateFilePath},
					},
					Env: []corev1.EnvVar{
						{Name: "TF_ENV_1", Value: "ENV-VALUE1"},
						{Name: "TF_ENV_2", ValueFrom: &corev1.EnvVarSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"}, Key: "TF_ENV_2"},
						}},
						{Name: "TF_ENV_3", ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"}, Key: "TF_ENV_3"},
						}},
					},
					Var: []corev1.EnvVar{
						{Name: "variable1", Value: "VAR-VALUE1"},
						{Name: "variable2", ValueFrom: &corev1.EnvVarSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"}, Key: "variable2"},
						}},
						{Name: "variable3", ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"}, Key: "variable3"},
						}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			// Setup FakeDelegation
			fakeClient := fake.NewSimpleClientset(
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-configmap",
						Namespace: moduleNamespace,
					},
					Data: map[string]string{
						"variable2": "VAR-VALUE2",
						"TF_ENV_2":  "ENV-VALUE2",
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: moduleNamespace,
					},
					Data: map[string][]byte{
						"variable3": []byte("VAR-VALUE3"),
						"TF_ENV_3":  []byte("ENV-VALUE3"),
					},
				},
			)
			testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), gomock.Any()).Return("token.X3", nil)
			testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X3").Return(fakeClient, nil)

			By("By making sure job was sent to jobQueue when commit hash is changed")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Running"))
			Expect(fetchedModule.Status.RunStartedAt.UTC()).Should(Equal(fakeClock.T.UTC()))
			Expect(fetchedModule.Status.RunDuration).Should(BeNil())
			Expect(fetchedModule.Status.ObservedGeneration).Should(Equal(fetchedModule.ObjectMeta.Generation))
			Expect(fetchedModule.Status.RunCommitHash).Should(Equal(commitHash))
			Expect(fetchedModule.Status.RunCommitMsg).Should(Equal(commitMsg))

			// advance time for testing
			fakeClock.T = time.Date(2022, 02, 01, 01, 01, 00, 0000, time.UTC)

			By("By making sure job run finished with expected envs and vars")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("Running")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Ready"))
			Expect(fetchedModule.Status.RunDuration.Duration.String()).Should(Equal("1m0s"))
			Expect(fetchedModule.Status.StateMessage).Should(ContainSubstring("Apply complete"))

			// Make sure LastDriftInfo & LastApplyInfo is also set
			Expect(fetchedModule.Status.LastDriftInfo.CommitHash).Should(Equal(commitHash))
			Expect(fetchedModule.Status.LastDriftInfo.Output).Should(ContainSubstring("Plan:"))
			Expect(fetchedModule.Status.LastDriftInfo.Timestamp.UTC()).Should(Equal(fakeClock.T.UTC()))

			Expect(fetchedModule.Status.LastApplyInfo.CommitHash).Should(Equal(commitHash))
			Expect(fetchedModule.Status.LastApplyInfo.Timestamp.UTC()).Should(Equal(fakeClock.T.UTC()))

			// make sure all values are there in output
			Expect(fetchedModule.Status.LastApplyInfo.Output).Should(ContainSubstring("ENV-VALUE1"))
			Expect(fetchedModule.Status.LastApplyInfo.Output).Should(ContainSubstring("ENV-VALUE2"))
			Expect(fetchedModule.Status.LastApplyInfo.Output).Should(ContainSubstring("ENV-VALUE3"))
			Expect(fetchedModule.Status.LastApplyInfo.Output).Should(ContainSubstring("VAR-VALUE1"))
			Expect(fetchedModule.Status.LastApplyInfo.Output).Should(ContainSubstring("VAR-VALUE2"))
			Expect(fetchedModule.Status.LastApplyInfo.Output).Should(ContainSubstring("VAR-VALUE3"))

			// make sure state file is created by local backend
			Expect(testStateFilePath).Should(BeAnExistingFile())
		})

		It("Should send module to job queue on commit change and runner should generate aws vault creds", func() {
			const (
				moduleName = "hello-with-aws-creds"
				path       = "hello"
			)

			By("By creating a new Module")
			ctx := context.Background()
			vaultReq := tfaplv1beta1.VaultRequests{
				AWS: &tfaplv1beta1.VaultAWSRequest{
					VaultRole: "aws-vault-role",
				},
			}
			module := &tfaplv1beta1.Module{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "terraform-applier.uw.systems/v1beta1",
					Kind:       "Module",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: moduleNamespace,
				},
				Spec: tfaplv1beta1.ModuleSpec{
					Schedule:      "50 * * * *",
					Path:          path,
					VaultRequests: &vaultReq,
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			// Setup FakeDelegation
			fakeClient := fake.NewSimpleClientset()
			testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), gomock.Any()).Return("token.X4", nil)
			testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X4").Return(fakeClient, nil)

			testVaultAWSConf.EXPECT().GenerateCreds("token.X4", gomock.Any()).
				Return(&vault.AWSCredentials{AccessKeyID: "AWS_KEY_ABCD1234", SecretAccessKey: "secret", Token: "token"}, nil)

			By("By making sure job was sent to jobQueue when commit hash is changed")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Running"))

			// advance time for testing
			fakeClock.T = time.Date(2022, 02, 01, 01, 01, 00, 0000, time.UTC)

			By("By making sure job run finished with expected AWS ENVs")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("Running")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Ready"))
			Expect(fetchedModule.Status.RunDuration.Duration.String()).Should(Equal("1m0s"))
			Expect(fetchedModule.Status.StateMessage).Should(ContainSubstring("Apply complete"))

			// make sure all values are there in output
			Expect(fetchedModule.Status.LastApplyInfo.Output).Should(ContainSubstring("AWS_KEY_ABCD1234"))
		})
	})

})

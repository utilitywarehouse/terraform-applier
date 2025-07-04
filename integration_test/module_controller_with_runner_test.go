package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	sbKeyringData, err := os.ReadFile(".tests_strongbox_keyring")
	if err != nil {
		fmt.Println(err)
		Panic()
	}

	sbIdentityData, err := os.ReadFile(".tests_strongbox_identity")
	if err != nil {
		fmt.Println(err)
		Panic()
	}

	Context("When creating Module", func() {
		BeforeEach(func() {
			// reset Time
			fakeClock.T = time.Date(2022, 02, 01, 01, 00, 00, 0000, time.UTC)
			testReconciler.Runner = &testRunner

			// remove any label selector
			testFilter.LabelSelectorKey = ""
			testFilter.LabelSelectorValue = ""

			// all jobs will be triggered automatically as they do not have initial commit hash
			testRepos.EXPECT().Hash(gomock.Any(), "https://host.xy/dummy/repo.git", "HEAD", "hello").
				Return(commitHash, nil).AnyTimes()
			testRepos.EXPECT().Subject(gomock.Any(), "https://host.xy/dummy/repo.git", commitHash).
				Return(commitMsg, nil).AnyTimes()

			var dst string
			testRepos.EXPECT().Clone(gomock.Any(), "https://host.xy/dummy/repo.git", gomock.AssignableToTypeOf(dst), "HEAD", nil, true).
				DoAndReturn(func(ctx context.Context, remote, dst, branch string, pathspecs []string, rmGitDir bool) (string, error) {
					return "commit124", os.CopyFS(dst, os.DirFS(filepath.Join("src", "modules")))
				}).AnyTimes()

			testMetrics.EXPECT().UpdateModuleRunDuration(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			testMetrics.EXPECT().UpdateModuleSuccess(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			testMetrics.EXPECT().SetRunPending(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

			// clear state file if exits
			os.Remove(testStateFilePath)
		})

		It("Should send module to job queue on initial runner should do plan & apply", func() {
			const (
				moduleName = "hello"
				repoURL    = "https://host.xy/dummy/repo.git"
				path       = "hello"
			)
			var lastRun, lastApplyRun *tfaplv1beta1.Run
			testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
					lastRun = run
					return nil
				})
			testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
					lastApplyRun = run
					return nil
				})

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
					RepoURL:  repoURL,
					Path:     path,
					Env: []tfaplv1beta1.EnvVar{
						{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
						{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			// Setup FakeDelegation
			fakeClient := fake.NewSimpleClientset()
			testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), gomock.Any()).Return("token.X1", nil)
			testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X1").Return(fakeClient, nil)

			By("By making sure job was sent to jobQueue")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Running"))
			Expect(fetchedModule.Status.LastDefaultRunStartedAt.UTC()).Should(Equal(fakeClock.T.UTC()))
			Expect(fetchedModule.Status.ObservedGeneration).Should(Equal(fetchedModule.ObjectMeta.Generation))
			Expect(fetchedModule.Status.LastDefaultRunCommitHash).Should(Equal(commitHash))

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

			Expect(fetchedModule.Status.CurrentState).Should(Equal(string(tfaplv1beta1.StatusOk)))

			// runner does clean up before updating redis
			time.Sleep(5 * time.Second)

			// Make sure LastDriftInfo & LastApplyInfo is also set
			Expect(lastRun.Output).Should(ContainSubstring("Plan:"))

			Expect(fetchedModule.Status.LastAppliedCommitHash).Should(Equal(commitHash))
			Expect(lastApplyRun.Output).Should(ContainSubstring("Apply complete!"))
			Expect(fetchedModule.Status.LastAppliedAt.UTC()).Should(Equal(fakeClock.T.UTC()))

			// make sure secret values are there in output (strongbox decryption was successful)
			Expect(lastApplyRun.Output).Should(ContainSubstring("TOP_SECRET_VALUE"))
			Expect(lastApplyRun.Output).Should(ContainSubstring("TOP_SECRET_VALUE_ENC_USING_AGE"))

			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should send module to job queue on initial run and runner should only do plan", func() {
			const (
				moduleName = "hello-plan-only"
				repoURL    = "https://host.xy/dummy/repo.git"
				path       = "hello"
			)

			var lastRun *tfaplv1beta1.Run
			testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
					lastRun = run
					return nil
				})

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
					RepoURL:  repoURL,
					Path:     path,
					PlanOnly: &boolTrue,
					Env: []tfaplv1beta1.EnvVar{
						{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
						{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			// Setup FakeDelegation
			fakeClient := fake.NewSimpleClientset()
			testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), gomock.Any()).Return("token.X2", nil)
			testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X2").Return(fakeClient, nil)

			By("By making sure job was sent to jobQueue")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Running"))
			Expect(fetchedModule.Status.LastDefaultRunStartedAt.UTC()).Should(Equal(fakeClock.T.UTC()))
			Expect(fetchedModule.Status.ObservedGeneration).Should(Equal(fetchedModule.ObjectMeta.Generation))
			Expect(fetchedModule.Status.LastDefaultRunCommitHash).Should(Equal(commitHash))

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

			Expect(fetchedModule.Status.CurrentState).Should(Equal(string(tfaplv1beta1.StatusDriftDetected)))

			// runner does clean up before updating redis
			time.Sleep(5 * time.Second)

			Expect(lastRun.Output).Should(ContainSubstring("Plan:"))

			// Make sure LastApplyInfo is also set
			Expect(fetchedModule.Status.LastAppliedCommitHash).Should(Equal(""))
			Expect(fetchedModule.Status.LastAppliedAt).Should(BeNil())

			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should send module to job queue on initial run and runner should read configmaps and secrets before apply and setup local backend", func() {
			const (
				moduleName = "hello-with-var-env"
				repoURL    = "https://host.xy/dummy/repo.git"
				path       = "hello"
			)

			var lastRun, lastApplyRun *tfaplv1beta1.Run
			testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
					lastRun = run
					return nil
				})
			testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
					lastApplyRun = run
					return nil
				})

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
					RepoURL:  repoURL,
					Path:     path,
					Backend: []tfaplv1beta1.EnvVar{
						{Name: "path", Value: testStateFilePath},
					},
					Env: []tfaplv1beta1.EnvVar{
						{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
						{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
						{Name: "TF_ENV_1", Value: "ENV-VALUE1"},
						{Name: "TF_ENV_2", ValueFrom: &tfaplv1beta1.EnvVarSource{
							ConfigMapKeyRef: &tfaplv1beta1.ConfigMapKeySelector{
								Name: "test-configmap", Key: "TF_ENV_2"},
						}},
						{Name: "TF_ENV_3", ValueFrom: &tfaplv1beta1.EnvVarSource{
							SecretKeyRef: &tfaplv1beta1.SecretKeySelector{
								Name: "test-secret", Key: "TF_ENV_3"},
						}},
					},
					Var: []tfaplv1beta1.EnvVar{
						{Name: "variable1", Value: "VAR-VALUE1"},
						{Name: "variable2", ValueFrom: &tfaplv1beta1.EnvVarSource{
							ConfigMapKeyRef: &tfaplv1beta1.ConfigMapKeySelector{
								Name: "test-configmap", Key: "variable2"},
						}},
						{Name: "variable3", ValueFrom: &tfaplv1beta1.EnvVarSource{
							SecretKeyRef: &tfaplv1beta1.SecretKeySelector{
								Name: "test-secret", Key: "variable3"},
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

			By("By making sure job was sent to jobQueue")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Not(Equal("")))

			Expect(fetchedModule.Status.CurrentState).Should(Equal("Running"))
			Expect(fetchedModule.Status.LastDefaultRunStartedAt.UTC()).Should(Equal(fakeClock.T.UTC()))
			Expect(fetchedModule.Status.ObservedGeneration).Should(Equal(fetchedModule.ObjectMeta.Generation))
			Expect(fetchedModule.Status.LastDefaultRunCommitHash).Should(Equal(commitHash))

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

			Expect(fetchedModule.Status.CurrentState).Should(Equal(string(tfaplv1beta1.StatusOk)))
			// runner does clean up before updating redis
			time.Sleep(5 * time.Second)
			Expect(lastRun.Output).Should(ContainSubstring("Plan:"))

			Expect(fetchedModule.Status.LastAppliedCommitHash).Should(Equal(commitHash))
			Expect(fetchedModule.Status.LastAppliedAt.UTC()).Should(Equal(fakeClock.T.UTC()))

			// make sure all values are there in output
			Expect(lastApplyRun.Output).Should(ContainSubstring("TOP_SECRET_VALUE"))
			Expect(lastApplyRun.Output).Should(ContainSubstring("TOP_SECRET_VALUE_ENC_USING_AGE"))
			Expect(lastApplyRun.Output).Should(ContainSubstring("ENV-VALUE1"))
			Expect(lastApplyRun.Output).Should(ContainSubstring("ENV-VALUE2"))
			Expect(lastApplyRun.Output).Should(ContainSubstring("ENV-VALUE3"))
			Expect(lastApplyRun.Output).Should(ContainSubstring("VAR-VALUE1"))
			Expect(lastApplyRun.Output).Should(ContainSubstring("VAR-VALUE2"))
			Expect(lastApplyRun.Output).Should(ContainSubstring("VAR-VALUE3"))

			// make sure state file is created by local backend
			Expect(testStateFilePath).Should(BeAnExistingFile())

			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should send module to job queue on initial run and runner should generate vault creds", func() {
			const (
				moduleName = "hello-with-vault-creds"
				repoURL    = "https://host.xy/dummy/repo.git"
				path       = "hello"
			)

			var lastRun, lastApplyRun *tfaplv1beta1.Run
			testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
					lastRun = run
					return nil
				})
			testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
					lastApplyRun = run
					return nil
				})

			By("By creating a new Module")
			ctx := context.Background()
			vaultReq := tfaplv1beta1.VaultRequests{
				AWS: &tfaplv1beta1.VaultAWSRequest{
					VaultRole: "aws-vault-role",
				},
				GCP: &tfaplv1beta1.VaultGCPRequest{
					StaticAccount: "gcp-vault-static-account",
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
					RepoURL:       repoURL,
					Path:          path,
					VaultRequests: &vaultReq,
					Env: []tfaplv1beta1.EnvVar{
						{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
						{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			// Setup FakeDelegation
			fakeClient := fake.NewSimpleClientset()
			testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), gomock.Any()).Return("token.X4", nil)
			testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X4").Return(fakeClient, nil)

			testVaultAWSConf.EXPECT().GenerateAWSCreds(gomock.Any(), "token.X4", gomock.Any()).
				Return(&vault.AWSCredentials{AccessKeyID: "AWS_KEY_ABCD1234", SecretAccessKey: "secret", Token: "token"}, nil)
			testVaultAWSConf.EXPECT().GenerateGCPToken(gomock.Any(), "token.X4", gomock.Any()).
				Return("ya29.c.c0ASRK0GZ2fzoXHQakYwhwQhSJZ3gFQT5V0Ro_E94zL3fo", nil)

			By("By making sure job was sent to jobQueue")
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

			Expect(fetchedModule.Status.CurrentState).Should(Equal(string(tfaplv1beta1.StatusOk)))

			// runner does clean up before updating redis
			time.Sleep(5 * time.Second)

			// make sure all values are there in output
			Expect(lastRun.Output).Should(ContainSubstring("AWS_KEY_ABCD1234"))
			Expect(lastApplyRun.Output).Should(ContainSubstring("AWS_KEY_ABCD1234"))

			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})
	})

})

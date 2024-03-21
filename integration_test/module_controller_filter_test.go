package integration_test

import (
	"context"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Module controller without runner with label selector", func() {

	const (
		moduleNamespace         = "default"
		interval                = time.Millisecond * 250
		commitHash              = "a1b2c3d4"
		commitMsg               = "test commit"
		labelSelectorKey        = "terraform-applier.uw.systems/test"
		labelSelectorKeyInvalid = "terraform-applier.uw.systems/test-invalid"
	)

	Context("When creating Module", func() {

		BeforeEach(func() {
			// reset Time
			fakeClock.T = time.Date(2022, 02, 01, 01, 00, 00, 0000, time.UTC)
			testReconciler.Queue = testFilterControllerQueue

			// add label selector
			testFilter.LabelSelectorKey = labelSelectorKey
			testFilter.LabelSelectorValue = "true"

			// Trigger Job run as soon as module is created
			testRepos.EXPECT().Hash(gomock.Any(), "https://host.xy/dummy/repo.git", "HEAD", "hello-filter-test").
				Return(commitHash, nil).AnyTimes()
			testRepos.EXPECT().LogMsg(gomock.Any(), "https://host.xy/dummy/repo.git", "HEAD", "hello-filter-test").
				Return(commitMsg, nil).AnyTimes()
		})

		It("Should send module with valid selector label selector to job queue", func() {
			const (
				moduleName = "filter-test-module1"
				repoURL    = "https://host.xy/dummy/repo.git"
				path       = "hello-filter-test"
			)

			By("By creating a new Module")
			ctx := context.Background()
			module := &tfaplv1beta1.Module{
				TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: moduleNamespace,
					Labels:    map[string]string{labelSelectorKey: "true"},
				},
				Spec: tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}

			By("By making sure job was sent to jobQueue")
			Eventually(func() types.NamespacedName {
				timer := time.NewTimer(time.Second)
				for {
					select {
					case req := <-testFilterControllerQueue:
						return req.NamespacedName
					case <-timer.C:
						return types.NamespacedName{}
					}
				}
			}, time.Second*10, interval).Should(Equal(moduleLookupKey))
			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should not send module with valid selector label key but invalid value to job queue", func() {
			const (
				moduleName = "filter-test-module2"
				repoURL    = "https://host.xy/dummy/repo.git"
				path       = "hello-filter-test"
			)

			By("By creating a new Module")
			ctx := context.Background()
			module := &tfaplv1beta1.Module{
				TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: moduleNamespace,
					Labels:    map[string]string{labelSelectorKey: "false"},
				},
				Spec: tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}

			By("By making sure job was not sent to jobQueue")
			Consistently(func() types.NamespacedName {
				timer := time.NewTimer(time.Second)
				for {
					select {
					case req := <-testFilterControllerQueue:
						return req.NamespacedName
					case <-timer.C:
						return types.NamespacedName{}
					}
				}
			}, time.Second*10, interval).Should(Not(Equal(moduleLookupKey)))
			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should not send module with missing selector label selector to job queue", func() {
			const (
				moduleName = "filter-test-module3"
				repoURL    = "https://host.xy/dummy/repo.git"
				path       = "hello-filter-test"
			)

			By("By creating a new Module")
			ctx := context.Background()
			module := &tfaplv1beta1.Module{
				TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: moduleNamespace,
					Labels:    map[string]string{labelSelectorKeyInvalid: "true"},
				},
				Spec: tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}

			By("By making sure job was not sent to jobQueue")
			Consistently(func() types.NamespacedName {
				timer := time.NewTimer(time.Second)
				for {
					select {
					case req := <-testFilterControllerQueue:
						return req.NamespacedName
					case <-timer.C:
						return types.NamespacedName{}
					}
				}
			}, time.Second*10, interval).Should(Not(Equal(moduleLookupKey)))
			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should not send module with mo labels to job queue", func() {
			const (
				moduleName = "filter-test-module4"
				repoURL    = "https://host.xy/dummy/repo.git"
				path       = "hello-filter-test"
			)

			By("By creating a new Module")
			ctx := context.Background()
			module := &tfaplv1beta1.Module{
				TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: moduleNamespace,
				},
				Spec: tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}

			By("By making sure job was not sent to jobQueue")
			Consistently(func() types.NamespacedName {
				timer := time.NewTimer(time.Second)
				for {
					select {
					case req := <-testFilterControllerQueue:
						return req.NamespacedName
					case <-timer.C:
						return types.NamespacedName{}
					}
				}
			}, time.Second*10, interval).Should(Not(Equal(moduleLookupKey)))
			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

	})
})

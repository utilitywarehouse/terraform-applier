package integration_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Module controller without runner", func() {

	const (
		moduleNamespace = "default"
		interval        = time.Millisecond * 250
	)

	Context("When creating Module", func() {

		BeforeEach(func() {
			// reset Time
			fakeClock.T = time.Date(2022, 02, 01, 01, 00, 00, 0000, time.UTC)
			testReconciler.Queue = testControllerQueue

			// remove any label selector
			testFilter.LabelSelectorKey = ""
			testFilter.LabelSelectorValue = ""
		})

		It("Should send module to job queue on schedule", func() {
			const (
				moduleName = "test-module"
				path       = "dev/" + moduleName
			)

			testGitUtil.
				EXPECT().
				HeadCommitHashAndLog(path).
				Return("", "", nil).MinTimes(1)

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
					Schedule: "1 * * * *",
					Path:     path,
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			// After creating this Module, let's check that the Module's Spec fields match what we passed in.
			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			createdModule := &tfaplv1beta1.Module{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, moduleLookupKey, createdModule)
				return err == nil
			}, time.Second*10, interval).Should(BeTrue())
			// Let's make sure our Schedule string value was properly converted/handled.
			Expect(createdModule.Spec.Schedule).Should(Equal("1 * * * *"))
			Expect(createdModule.Spec.Path).Should(Equal(path))
			// check default values
			Expect(createdModule.Spec.PollInterval).Should(Equal(60))
			Expect(createdModule.Spec.DelegateServiceAccountSecretRef).Should(Equal("terraform-applier-delegate-token"))
			Expect(createdModule.Spec.RunTimeout).Should(Equal(900))

			// trick controller to accept mocked test time as earliestTime as we cannot control created time
			module.Status.RunStartedAt = &metav1.Time{Time: time.Date(2022, 02, 01, 01, 00, 30, 0000, time.UTC)}
			Expect(k8sClient.Status().Update(ctx, module)).Should(Succeed())

			By("By making sure job is not sent to job queue before schedule")
			fakeClock.T = time.Date(2022, 02, 01, 01, 00, 40, 0000, time.UTC)
			Consistently(func() types.NamespacedName {
				timer := time.NewTimer(time.Second)
				for {
					select {
					case req := <-testControllerQueue:
						return req.NamespacedName
					case <-timer.C:
						return types.NamespacedName{}
					}
				}
			}, time.Second*15, interval).Should(Equal(types.NamespacedName{}))

			// advance time
			By("By making sure job was sent to jobQueue at schedule after advancing time")
			fakeClock.T = time.Date(2022, 02, 01, 01, 01, 00, 0000, time.UTC)
			Eventually(func() types.NamespacedName {
				timer := time.NewTimer(time.Second)
				for {
					select {
					case req := <-testControllerQueue:
						return req.NamespacedName
					case <-timer.C:
						return types.NamespacedName{}
					}
				}
			}, time.Second*60, interval).Should(Equal(moduleLookupKey))
			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should send module to job queue on commit change", func() {
			const (
				moduleName = "test-module2"
				path       = "dev/" + moduleName
			)
			testGitUtil.EXPECT().HeadCommitHashAndLog(path).Return("", "", nil)

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
					Schedule: "1 * * * *",
					Path:     path,
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}

			By("By making sure job was sent to jobQueue when commit hash is changed")
			testGitUtil.EXPECT().HeadCommitHashAndLog(path).Return("1234abcd", "test commit", nil)
			// wait for just about 60 sec default poll interval
			Eventually(func() types.NamespacedName {
				timer := time.NewTimer(time.Second)
				for {
					select {
					case req := <-testControllerQueue:
						return req.NamespacedName
					case <-timer.C:
						return types.NamespacedName{}
					}
				}
			}, time.Second*70, interval).Should(Equal(moduleLookupKey))
			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should not trigger run for module with invalid schedule", func() {
			const (
				moduleName = "test-module3"
				path       = "dev/" + moduleName
			)
			testGitUtil.EXPECT().HeadCommitHashAndLog(path).Return("", "", nil).AnyTimes()

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
					Schedule: "1 * * *",
					Path:     path,
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			By("By making sure modules status is changed to errored")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Equal("Errored"))
			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should not trigger run for module with git error", func() {
			const (
				moduleName = "test-module4"
				path       = "dev/" + moduleName
			)
			testGitUtil.EXPECT().HeadCommitHashAndLog(path).Return("", "", fmt.Errorf("generating test error")).AnyTimes()

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
					Schedule: "1 * * * *",
					Path:     path,
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			By("By making sure modules status is changed to errored")
			Eventually(func() string {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				if err != nil {
					return ""
				}
				return fetchedModule.Status.CurrentState
			}, time.Second*30, interval).Should(Equal("Errored"))
			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

		It("Should not trigger run for suspended module", func() {
			const (
				moduleName = "test-module5"
				path       = "dev/" + moduleName
			)
			var boolTrue = true
			testGitUtil.EXPECT().HeadCommitHashAndLog(path).Return("", "", nil).AnyTimes()

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
					Schedule: "1 * * * *",
					Path:     path,
				},
			}
			Expect(k8sClient.Create(ctx, module)).Should(Succeed())

			// After creating this Module, let's check that the Module's Spec fields match what we passed in.
			moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule := &tfaplv1beta1.Module{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, moduleLookupKey, fetchedModule)
				return err == nil
			}, time.Second*10, interval).Should(BeTrue())

			// trick controller to accept mocked test time as earliestTime as we cannot control created time
			fetchedModule.Status.RunStartedAt = &metav1.Time{Time: time.Date(2022, 02, 01, 01, 00, 30, 0000, time.UTC)}
			Expect(k8sClient.Status().Update(ctx, fetchedModule)).Should(Succeed())

			// let controller reschedule
			time.Sleep(10 * time.Second)

			By("By suspending Module")
			// moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}
			fetchedModule.Spec.Suspend = &boolTrue
			Expect(k8sClient.Update(ctx, fetchedModule)).Should(Succeed())

			// advance time
			By("By making sure job was never sent to jobQueue after advancing time")
			fakeClock.T = time.Date(2022, 02, 01, 01, 01, 00, 0000, time.UTC)
			Consistently(func() types.NamespacedName {
				timer := time.NewTimer(time.Second)
				for {
					select {
					case req := <-testControllerQueue:
						return req.NamespacedName
					case <-timer.C:
						return types.NamespacedName{}
					}
				}
			}, time.Second*20, interval).Should(Equal(types.NamespacedName{}))
			// delete module to stopping requeue
			Expect(k8sClient.Delete(ctx, module)).Should(Succeed())
		})

	})
})

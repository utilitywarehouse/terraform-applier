package sysutil

import (
	"context"
	"fmt"
	"time"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	k8sAPICallRetryInterval = 5 * time.Second // How much time to wait in between retrying a k8s API call
	k8sAPICallRetryTimeout  = 5 * time.Minute // How long to wait until we determine that the k8s API is definitively unavailable
)

var (
	retriableError = func(err error) bool {
		return errors.IsConflict(err) ||
			errors.IsServiceUnavailable(err) ||
			errors.IsServerTimeout(err) ||
			errors.IsTimeout(err) ||
			errors.IsTooManyRequests(err)
	}
)

// CallWithBackOff uses wait.PollUntilContextTimeout to retry
// this function should be used to get object
func PollUntilTimeout(ctx context.Context, fn func(ctx context.Context) error) error {
	var apiErr error
	err := wait.PollUntilContextTimeout(ctx, k8sAPICallRetryInterval, k8sAPICallRetryTimeout, true, func(ctx context.Context) (bool, error) {
		apiErr = fn(ctx)
		switch {
		case apiErr == nil:
			return true, nil
		case retriableError(apiErr):
			return false, nil
		default:
			return false, apiErr
		}
	})
	if wait.Interrupted(err) {
		err = apiErr
	}
	return err
}

// CallWithBackOff uses wait.ExponentialBackoffWithContext with default retry
// this function should be used to update or patch object
func CallWithBackOff(ctx context.Context, fn func(ctx context.Context) error) error {
	var apiErr error
	err := wait.ExponentialBackoffWithContext(ctx, retry.DefaultRetry, func(ctx context.Context) (bool, error) {
		apiErr = fn(ctx)
		switch {
		case apiErr == nil:
			return true, nil
		case retriableError(apiErr):
			return false, nil
		default:
			return false, apiErr
		}
	})
	if wait.Interrupted(err) {
		err = apiErr
	}
	return err
}

// GetModule will use PollUntilContextTimeout to get requested module
func GetModule(ctx context.Context, client client.Client, key types.NamespacedName) (*tfaplv1beta1.Module, error) {
	module := new(tfaplv1beta1.Module)

	err := PollUntilTimeout(ctx, func(ctx context.Context) (err error) {
		return client.Get(ctx, key, module)
	})
	if err != nil {
		return nil, fmt.Errorf("timed out trying to get module err:%w", err)
	}
	return module, nil
}

// GetSecret will use PollUntilContextTimeout to get requested secret
func GetSecret(ctx context.Context, client kubernetes.Interface, namespace, name string) (*corev1.Secret, error) {
	var secret *corev1.Secret

	err := PollUntilTimeout(ctx, func(ctx context.Context) (err error) {
		secret, err = client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("timed out trying to get secret err:%w", err)
	}
	return secret, nil
}

// GetConfigMaps will use PollUntilContextTimeout to get requested ConfigMaps
func GetConfigMaps(ctx context.Context, client kubernetes.Interface, namespace, name string) (*corev1.ConfigMap, error) {
	var cm *corev1.ConfigMap

	err := PollUntilTimeout(ctx, func(ctx context.Context) (err error) {
		cm, err = client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("timed out trying to get configMap err:%w", err)
	}

	return cm, nil
}

// PatchModuleStatus will re-try patching with back-off
func PatchModuleStatus(ctx context.Context, c client.Client, objectKey types.NamespacedName, newStatus tfaplv1beta1.ModuleStatus) error {
	tryPatch := func(ctx context.Context) error {
		// refetch module on every try, since
		// if you got a conflict on the last update attempt then you need to get
		// the current version before making your own changes.
		module, err := GetModule(ctx, c, objectKey)
		if err != nil {
			return err
		}

		// Make whatever updates to the resource are needed
		patch := client.MergeFrom(module.DeepCopy())
		module.Status = newStatus

		// You have to return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return c.Status().Patch(ctx, module, patch, client.FieldOwner("terraform-applier"))
	}

	err := CallWithBackOff(ctx, tryPatch)
	if err != nil {
		return fmt.Errorf("unable to set status, max attempted reached err:%w", err)
	}

	return nil
}

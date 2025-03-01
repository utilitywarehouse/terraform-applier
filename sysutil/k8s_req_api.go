package sysutil

import (
	"context"
	"encoding/json"
	"fmt"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnsureRequest will try to add run request annotation with back-off
func EnsureRequest(ctx context.Context, client client.Client, key types.NamespacedName, req *tfaplv1beta1.Request) error {
	if err := req.Validate(); err != nil {
		return err
	}

	tryUpdate := func(ctx context.Context) error {
		// refetch module on every try
		module, err := GetModule(ctx, client, key)
		if err != nil {
			return err
		}

		existingReq, err := module.PendingRunRequest()
		// if valid req found then verify if its matching given req
		if err == nil && existingReq != nil {
			if req.RequestedAt.Equal(existingReq.RequestedAt) {
				return nil
			} else {
				return tfaplv1beta1.ErrRunRequestExist
			}
		}

		if module.ObjectMeta.Annotations == nil {
			module.ObjectMeta.Annotations = make(map[string]string)
		}
		valueBytes, err := json.Marshal(&req)
		if err != nil {
			return err
		}
		module.ObjectMeta.Annotations[tfaplv1beta1.RunRequestAnnotationKey] = string(valueBytes)

		// return err itself here (not wrapped inside another error)
		// so that ExponentialBackoffWithContext can identify it correctly.
		return client.Update(ctx, module)
	}

	err := CallWithBackOff(ctx, tryUpdate)
	if err != nil {
		return fmt.Errorf("unable to set run request err:%w", err)
	}

	return nil
}

// RemoveRequest will try to remove given run request
// it will error if given request id doesn't match existing pending request ID
func RemoveRequest(ctx context.Context, client client.Client, key types.NamespacedName, req *tfaplv1beta1.Request) error {
	tryUpdate := func(ctx context.Context) error {
		// refetch module on every try
		module, err := GetModule(ctx, client, key)
		if err != nil {
			return err
		}

		existingReq, err := module.PendingRunRequest()
		if err == nil {
			if existingReq == nil {
				return tfaplv1beta1.ErrNoRunRequestFound
			}

			if !req.RequestedAt.Equal(existingReq.RequestedAt) {
				return tfaplv1beta1.ErrRunRequestMismatch
			}
		}

		delete(module.ObjectMeta.Annotations, tfaplv1beta1.RunRequestAnnotationKey)

		// return err itself here (not wrapped inside another error)
		// so that ExponentialBackoffWithContext can identify it correctly.
		return client.Update(ctx, module)
	}

	err := CallWithBackOff(ctx, tryUpdate)
	if err != nil {
		return fmt.Errorf("unable to remove pending run request err:%w", err)
	}

	return nil
}

// RemoveCurrentRequest will try to remove current request without validation
func RemoveCurrentRequest(ctx context.Context, client client.Client, key types.NamespacedName) error {
	tryUpdate := func(ctx context.Context) error {
		// refetch module on every try
		module, err := GetModule(ctx, client, key)
		if err != nil {
			return err
		}

		if _, exists := module.ObjectMeta.Annotations[tfaplv1beta1.RunRequestAnnotationKey]; !exists {
			return nil
		}

		delete(module.ObjectMeta.Annotations, tfaplv1beta1.RunRequestAnnotationKey)

		// return err itself here (not wrapped inside another error)
		// so that ExponentialBackoffWithContext can identify it correctly.
		return client.Update(ctx, module)
	}

	err := CallWithBackOff(ctx, tryUpdate)
	if err != nil {
		return fmt.Errorf("unable to remove pending run request err:%w", err)
	}

	return nil
}

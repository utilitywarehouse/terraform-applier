package sysutil

import (
	"context"
	"encoding/json"
	"fmt"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnsureRequest will try to add run request annotation with back-off
func EnsureRequest(ctx context.Context, client client.Client, req *tfaplv1beta1.Request) error {
	if err := req.Validate(); err != nil {
		return err
	}

	tryUpdate := func(ctx context.Context) error {
		// refetch module on every try
		module, err := GetModule(ctx, client, req.NamespacedName)
		if err != nil {
			return err
		}

		existingReq, ok := module.PendingRunRequest()
		if ok {
			// if annotated request ID is matching then nothing to do
			if req.ID == existingReq.ID {
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
		module.ObjectMeta.Annotations[tfaplv1beta1.TriggerRunAnnotationKey] = string(valueBytes)

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
func RemoveRequest(ctx context.Context, client client.Client, req *tfaplv1beta1.Request) error {
	tryUpdate := func(ctx context.Context) error {
		// refetch module on every try
		module, err := GetModule(ctx, client, req.NamespacedName)
		if err != nil {
			return err
		}

		existingReq, ok := module.PendingRunRequest()
		if !ok {
			return tfaplv1beta1.ErrNoRunRequestFound
		}

		if req.ID != existingReq.ID {
			return tfaplv1beta1.ErrRunRequestMismatch
		}

		delete(module.ObjectMeta.Annotations, tfaplv1beta1.TriggerRunAnnotationKey)

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

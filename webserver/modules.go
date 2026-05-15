package webserver

import (
	"context"
	"fmt"
	"slices"
	"sort"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// namespace stores the current state of the waybill and events of a namespace.
type Namespace struct {
	Modules []Module
	// Events        []corev1.Event
	DiffURLFormat string
}

type Module struct {
	Module tfaplv1beta1.Module
	Runs   []*tfaplv1beta1.Run
	Events []corev1.Event
}

func createNamespaceMap(modules []tfaplv1beta1.Module) map[string]*Namespace {
	namespaces := make(map[string]*Namespace)

	for _, m := range modules {
		_, ok := namespaces[m.Namespace]
		if !ok {
			namespaces[m.Namespace] = &Namespace{}
		}
		module := Module{Module: m}

		namespaces[m.Namespace].Modules = append(namespaces[m.Namespace].Modules, module)
	}

	return namespaces
}

func listModules(ctx context.Context, clt client.Client) ([]tfaplv1beta1.Module, error) {
	moduleList := &tfaplv1beta1.ModuleList{}

	err := clt.List(ctx, moduleList)
	if err != nil {
		return nil, err
	}

	sort.Slice(moduleList.Items, func(i, j int) bool {
		return moduleList.Items[i].Namespace+moduleList.Items[i].Name < moduleList.Items[j].Namespace+moduleList.Items[j].Name
	})
	return moduleList.Items, nil
}

func moduleWithRunsInfo(ctx context.Context, clt client.Client, kubeClient kubernetes.Interface, redis sysutil.RedisInterface, namespacedName types.NamespacedName) (*Module, error) {
	var m tfaplv1beta1.Module

	err := clt.Get(ctx, namespacedName, &m)
	if err != nil {
		return nil, err
	}

	module := Module{Module: m}

	module.Runs = runInfo(ctx, redis, namespacedName)

	// get events
	fieldSelector := fmt.Sprintf("involvedObject.kind=Module,involvedObject.name=%s", namespacedName.Name)
	eventList, err := kubeClient.CoreV1().Events(namespacedName.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return &module, nil
	}

	module.Events = eventList.Items

	// Sort events by LastTimestamp descending
	sort.Slice(module.Events, func(i, j int) bool {
		return module.Events[j].LastTimestamp.Before(&module.Events[i].LastTimestamp)
	})

	return &module, nil
}

func runInfo(ctx context.Context, redis sysutil.RedisInterface, namespacedName types.NamespacedName) []*tfaplv1beta1.Run {
	// error can be skipped here
	runs, _ := redis.Runs(ctx, namespacedName, "*")

	// sort runs by StartedAt DESC
	slices.SortFunc(runs, func(a *tfaplv1beta1.Run, b *tfaplv1beta1.Run) int {
		if a != nil && b != nil &&
			a.StartedAt != nil && b.StartedAt != nil {
			return b.StartedAt.Compare(a.StartedAt.Time)
		}
		return 0
	})
	// remove duplicate runs (scenario when last run is also a apply run)
	runs = slices.CompactFunc(runs, func(a *tfaplv1beta1.Run, b *tfaplv1beta1.Run) bool {
		if a != nil && b != nil &&
			a.StartedAt != nil && b.StartedAt != nil {
			return a.StartedAt.Compare(b.StartedAt.Time) == 0
		}
		return false
	})

	return runs
}

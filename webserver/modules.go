package webserver

import (
	"context"
	"slices"
	"sort"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
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
}

func createNamespaceMap(ctx context.Context, modules []tfaplv1beta1.Module, redis sysutil.RedisInterface) map[string]*Namespace {
	namespaces := make(map[string]*Namespace)

	for _, m := range modules {
		_, ok := namespaces[m.Namespace]
		if !ok {
			namespaces[m.Namespace] = &Namespace{}
		}
		module := Module{Module: m}

		// error can be skipped here
		module.Runs, _ = redis.Runs(ctx, m.NamespacedName(), "*")

		// sort runs by StartedAt DESC
		slices.SortFunc(module.Runs, func(a *tfaplv1beta1.Run, b *tfaplv1beta1.Run) int {
			if a != nil && b != nil &&
				a.StartedAt != nil && b.StartedAt != nil {
				return b.StartedAt.Compare(a.StartedAt.Time)
			}
			return 0
		})
		// remove duplicate runs (scenario when last run is also a apply run)
		module.Runs = slices.CompactFunc(module.Runs, func(a *tfaplv1beta1.Run, b *tfaplv1beta1.Run) bool {
			if a != nil && b != nil &&
				a.StartedAt != nil && b.StartedAt != nil {
				return a.StartedAt.Compare(b.StartedAt.Time) == 0
			}
			return false
		})

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

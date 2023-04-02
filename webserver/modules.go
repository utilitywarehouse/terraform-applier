package webserver

import (
	"context"
	"sort"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// namespace stores the current state of the waybill and events of a namespace.
type Namespace struct {
	Modules []tfaplv1beta1.Module
	// Events        []corev1.Event
	DiffURLFormat string
}

func createNamespaceMap(modules []tfaplv1beta1.Module) map[string]*Namespace {
	namespaces := make(map[string]*Namespace)

	for _, m := range modules {
		_, ok := namespaces[m.Namespace]
		if !ok {
			namespaces[m.Namespace] = &Namespace{}
		}
		namespaces[m.Namespace].Modules = append(namespaces[m.Namespace].Modules, m)
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

// HasAccess returns a boolean depending on whether the email address provided
// corresponds to a user who has edit access to the specified Module.
func hasAccess(ctx context.Context, c kubernetes.Interface, module *tfaplv1beta1.Module, email, verb string) (bool, error) {
	gvk := module.GroupVersionKind()
	plural := "modules"

	response, err := c.AuthorizationV1().SubjectAccessReviews().Create(
		ctx,
		&authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: module.Namespace,
					Verb:      verb,
					Group:     gvk.Group,
					Version:   gvk.Version,
					Resource:  plural,
				},
				User: email,
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return false, err
	}
	return response.Status.Allowed, nil
}

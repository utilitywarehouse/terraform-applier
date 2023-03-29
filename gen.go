package main

//Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
//go:generate go run sigs.k8s.io/controller-tools/cmd/controller-gen rbac:roleName=terraform-applier crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

//Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
//go:generate go run sigs.k8s.io/controller-tools/cmd/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."

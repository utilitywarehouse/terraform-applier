//go:build tools

package main

import (
	_ "github.com/golang/mock/mockgen"
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"
)

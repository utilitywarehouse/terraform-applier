package integration_test

import (
	. "github.com/onsi/ginkgo/v2"
)

// To recover from panics in go routine during test mostly because of gomock
// https://github.com/onsi/ginkgo/issues/813#issuecomment-851685310
func RecoveringGinkgoT(optionalOffset ...int) GinkgoTInterface {
	return recoveringGinkgoT{GinkgoT(optionalOffset...)}
}

type recoveringGinkgoT struct {
	GinkgoTInterface
}

func (t recoveringGinkgoT) Error(args ...interface{}) {
	defer GinkgoRecover()
	t.GinkgoTInterface.Error(args...)
}

func (t recoveringGinkgoT) Errorf(format string, args ...interface{}) {
	defer GinkgoRecover()
	t.GinkgoTInterface.Errorf(format, args...)
}

func (t recoveringGinkgoT) Fail() {
	defer GinkgoRecover()
	t.GinkgoTInterface.Fail()
}

func (t recoveringGinkgoT) FailNow() {
	defer GinkgoRecover()
	t.GinkgoTInterface.FailNow()
}

func (t recoveringGinkgoT) Fatal(args ...interface{}) {
	defer GinkgoRecover()
	t.GinkgoTInterface.Fatal(args...)
}

func (t recoveringGinkgoT) Fatalf(format string, args ...interface{}) {
	defer GinkgoRecover()
	t.GinkgoTInterface.Fatalf(format, args...)
}

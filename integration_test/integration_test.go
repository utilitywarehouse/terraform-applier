package integration_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
)

// To recover from panics in go routine during test mostly because of gomock
// https://github.com/onsi/ginkgo/issues/813#issuecomment-851685310
func RecoveringGinkgoT() GinkgoTInterface {
	return recoveringGinkgoT{GinkgoT()}
}

type recoveringGinkgoT struct {
	GinkgoT GinkgoTInterface
}

func (t recoveringGinkgoT) Cleanup(f func()) {
	t.GinkgoT.Cleanup(f)
}

func (t recoveringGinkgoT) Error(args ...interface{}) {
	defer GinkgoRecover()
	t.GinkgoT.Error(args...)
}

func (t recoveringGinkgoT) Errorf(format string, args ...interface{}) {
	defer GinkgoRecover()
	t.GinkgoT.Errorf(format, args...)
}

func (t recoveringGinkgoT) Fail() {
	defer GinkgoRecover()
	t.GinkgoT.Fail()
}

func (t recoveringGinkgoT) FailNow() {
	defer GinkgoRecover()
	t.GinkgoT.FailNow()
}

func (t recoveringGinkgoT) Failed() bool {
	return t.GinkgoT.Failed()
}

func (t recoveringGinkgoT) Fatal(args ...interface{}) {
	defer GinkgoRecover()
	fmt.Println(args...)
	t.GinkgoT.Fatal(args...)
}

func (t recoveringGinkgoT) Fatalf(format string, args ...interface{}) {
	defer GinkgoRecover()
	fmt.Printf(format+"\n", args...)
	t.GinkgoT.Fatalf(format, args...)
}

func (t recoveringGinkgoT) Helper() {
	t.GinkgoT.Helper()
}

func (t recoveringGinkgoT) Log(args ...interface{}) {
	t.GinkgoT.Log(args...)
	// fmt.Fprintln(t.writer, args...)
}

func (t recoveringGinkgoT) Logf(format string, args ...interface{}) {
	t.GinkgoT.Logf(format, args...)
}

func (t recoveringGinkgoT) Name() string {
	return t.GinkgoT.Name()
}

func (t recoveringGinkgoT) Parallel() {
	t.GinkgoT.Parallel()
}

func (t recoveringGinkgoT) Setenv(kev string, value string) {
	t.GinkgoT.Setenv(kev, value)
}

func (t recoveringGinkgoT) Skip(args ...interface{}) {
	t.GinkgoT.Skip(args...)
}

func (t recoveringGinkgoT) SkipNow() {
	t.GinkgoT.SkipNow()
}

func (t recoveringGinkgoT) Skipf(format string, args ...interface{}) {
	t.GinkgoT.Skipf(format, args...)
}

func (t recoveringGinkgoT) Skipped() bool {
	return t.GinkgoT.Skipped()
}

func (t recoveringGinkgoT) TempDir() string {
	return t.GinkgoT.TempDir()
}

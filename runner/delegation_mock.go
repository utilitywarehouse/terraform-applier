// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/utilitywarehouse/terraform-applier/runner (interfaces: DelegateInterface)

// Package runner is a generated GoMock package.
package runner

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	v1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	kubernetes "k8s.io/client-go/kubernetes"
)

// MockDelegateInterface is a mock of DelegateInterface interface.
type MockDelegateInterface struct {
	ctrl     *gomock.Controller
	recorder *MockDelegateInterfaceMockRecorder
}

// MockDelegateInterfaceMockRecorder is the mock recorder for MockDelegateInterface.
type MockDelegateInterfaceMockRecorder struct {
	mock *MockDelegateInterface
}

// NewMockDelegateInterface creates a new mock instance.
func NewMockDelegateInterface(ctrl *gomock.Controller) *MockDelegateInterface {
	mock := &MockDelegateInterface{ctrl: ctrl}
	mock.recorder = &MockDelegateInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockDelegateInterface) EXPECT() *MockDelegateInterfaceMockRecorder {
	return m.recorder
}

// SetupDelegation mocks base method.
func (m *MockDelegateInterface) SetupDelegation(arg0 context.Context, arg1 kubernetes.Interface, arg2 *v1beta1.Module) (kubernetes.Interface, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetupDelegation", arg0, arg1, arg2)
	ret0, _ := ret[0].(kubernetes.Interface)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SetupDelegation indicates an expected call of SetupDelegation.
func (mr *MockDelegateInterfaceMockRecorder) SetupDelegation(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetupDelegation", reflect.TypeOf((*MockDelegateInterface)(nil).SetupDelegation), arg0, arg1, arg2)
}

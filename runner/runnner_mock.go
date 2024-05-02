// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/utilitywarehouse/terraform-applier/runner (interfaces: RunnerInterface)

// Package runner is a generated GoMock package.
package runner

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	v1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

// MockRunnerInterface is a mock of RunnerInterface interface.
type MockRunnerInterface struct {
	ctrl     *gomock.Controller
	recorder *MockRunnerInterfaceMockRecorder
}

// MockRunnerInterfaceMockRecorder is the mock recorder for MockRunnerInterface.
type MockRunnerInterfaceMockRecorder struct {
	mock *MockRunnerInterface
}

// NewMockRunnerInterface creates a new mock instance.
func NewMockRunnerInterface(ctrl *gomock.Controller) *MockRunnerInterface {
	mock := &MockRunnerInterface{ctrl: ctrl}
	mock.recorder = &MockRunnerInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRunnerInterface) EXPECT() *MockRunnerInterfaceMockRecorder {
	return m.recorder
}

// Start mocks base method.
func (m *MockRunnerInterface) Start(arg0 *v1beta1.Run, arg1 chan struct{}) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Start", arg0, arg1)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Start indicates an expected call of Start.
func (mr *MockRunnerInterfaceMockRecorder) Start(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Start", reflect.TypeOf((*MockRunnerInterface)(nil).Start), arg0, arg1)
}
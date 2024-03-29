// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/utilitywarehouse/terraform-applier/runner (interfaces: TFExecuter)

// Package runner is a generated GoMock package.
package runner

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
)

// MockTFExecuter is a mock of TFExecuter interface.
type MockTFExecuter struct {
	ctrl     *gomock.Controller
	recorder *MockTFExecuterMockRecorder
}

// MockTFExecuterMockRecorder is the mock recorder for MockTFExecuter.
type MockTFExecuterMockRecorder struct {
	mock *MockTFExecuter
}

// NewMockTFExecuter creates a new mock instance.
func NewMockTFExecuter(ctrl *gomock.Controller) *MockTFExecuter {
	mock := &MockTFExecuter{ctrl: ctrl}
	mock.recorder = &MockTFExecuterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTFExecuter) EXPECT() *MockTFExecuterMockRecorder {
	return m.recorder
}

// apply mocks base method.
func (m *MockTFExecuter) apply(arg0 context.Context) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "apply", arg0)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// apply indicates an expected call of apply.
func (mr *MockTFExecuterMockRecorder) apply(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "apply", reflect.TypeOf((*MockTFExecuter)(nil).apply), arg0)
}

// cleanUp mocks base method.
func (m *MockTFExecuter) cleanUp() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "cleanUp")
}

// cleanUp indicates an expected call of cleanUp.
func (mr *MockTFExecuterMockRecorder) cleanUp() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "cleanUp", reflect.TypeOf((*MockTFExecuter)(nil).cleanUp))
}

// init mocks base method.
func (m *MockTFExecuter) init(arg0 context.Context, arg1 map[string]string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "init", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// init indicates an expected call of init.
func (mr *MockTFExecuterMockRecorder) init(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "init", reflect.TypeOf((*MockTFExecuter)(nil).init), arg0, arg1)
}

// plan mocks base method.
func (m *MockTFExecuter) plan(arg0 context.Context) (bool, string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "plan", arg0)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(string)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// plan indicates an expected call of plan.
func (mr *MockTFExecuterMockRecorder) plan(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "plan", reflect.TypeOf((*MockTFExecuter)(nil).plan), arg0)
}

// showPlanFileRaw mocks base method.
func (m *MockTFExecuter) showPlanFileRaw(arg0 context.Context) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "showPlanFileRaw", arg0)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// showPlanFileRaw indicates an expected call of showPlanFileRaw.
func (mr *MockTFExecuterMockRecorder) showPlanFileRaw(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "showPlanFileRaw", reflect.TypeOf((*MockTFExecuter)(nil).showPlanFileRaw), arg0)
}

// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/utilitywarehouse/terraform-applier/sysutil (interfaces: RedisInterface)

// Package sysutil is a generated GoMock package.
package sysutil

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	v1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	types "k8s.io/apimachinery/pkg/types"
)

// MockRedisInterface is a mock of RedisInterface interface.
type MockRedisInterface struct {
	ctrl     *gomock.Controller
	recorder *MockRedisInterfaceMockRecorder
}

// MockRedisInterfaceMockRecorder is the mock recorder for MockRedisInterface.
type MockRedisInterfaceMockRecorder struct {
	mock *MockRedisInterface
}

// NewMockRedisInterface creates a new mock instance.
func NewMockRedisInterface(ctrl *gomock.Controller) *MockRedisInterface {
	mock := &MockRedisInterface{ctrl: ctrl}
	mock.recorder = &MockRedisInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRedisInterface) EXPECT() *MockRedisInterfaceMockRecorder {
	return m.recorder
}

// DefaultApply mocks base method.
func (m *MockRedisInterface) DefaultApply(arg0 context.Context, arg1 types.NamespacedName) (*v1beta1.Run, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DefaultApply", arg0, arg1)
	ret0, _ := ret[0].(*v1beta1.Run)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DefaultApply indicates an expected call of DefaultApply.
func (mr *MockRedisInterfaceMockRecorder) DefaultApply(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DefaultApply", reflect.TypeOf((*MockRedisInterface)(nil).DefaultApply), arg0, arg1)
}

// DefaultLastRun mocks base method.
func (m *MockRedisInterface) DefaultLastRun(arg0 context.Context, arg1 types.NamespacedName) (*v1beta1.Run, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DefaultLastRun", arg0, arg1)
	ret0, _ := ret[0].(*v1beta1.Run)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DefaultLastRun indicates an expected call of DefaultLastRun.
func (mr *MockRedisInterfaceMockRecorder) DefaultLastRun(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DefaultLastRun", reflect.TypeOf((*MockRedisInterface)(nil).DefaultLastRun), arg0, arg1)
}

// GetCommitHash mocks base method.
func (m *MockRedisInterface) GetCommitHash(arg0 context.Context, arg1 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCommitHash", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetCommitHash indicates an expected call of GetCommitHash.
func (mr *MockRedisInterfaceMockRecorder) GetCommitHash(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCommitHash", reflect.TypeOf((*MockRedisInterface)(nil).GetCommitHash), arg0, arg1)
}

// PRRun mocks base method.
func (m *MockRedisInterface) PRRun(arg0 context.Context, arg1 types.NamespacedName, arg2 int, arg3 string) (*v1beta1.Run, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PRRun", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(*v1beta1.Run)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PRRun indicates an expected call of PRRun.
func (mr *MockRedisInterfaceMockRecorder) PRRun(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PRRun", reflect.TypeOf((*MockRedisInterface)(nil).PRRun), arg0, arg1, arg2, arg3)
}

// Run mocks base method.
func (m *MockRedisInterface) Run(arg0 context.Context, arg1 string) (*v1beta1.Run, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Run", arg0, arg1)
	ret0, _ := ret[0].(*v1beta1.Run)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Run indicates an expected call of Run.
func (mr *MockRedisInterfaceMockRecorder) Run(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*MockRedisInterface)(nil).Run), arg0, arg1)
}

// Runs mocks base method.
func (m *MockRedisInterface) Runs(arg0 context.Context, arg1 types.NamespacedName, arg2 string) ([]*v1beta1.Run, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Runs", arg0, arg1, arg2)
	ret0, _ := ret[0].([]*v1beta1.Run)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Runs indicates an expected call of Runs.
func (mr *MockRedisInterfaceMockRecorder) Runs(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Runs", reflect.TypeOf((*MockRedisInterface)(nil).Runs), arg0, arg1, arg2)
}

// SetDefaultApply mocks base method.
func (m *MockRedisInterface) SetDefaultApply(arg0 context.Context, arg1 *v1beta1.Run) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetDefaultApply", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetDefaultApply indicates an expected call of SetDefaultApply.
func (mr *MockRedisInterfaceMockRecorder) SetDefaultApply(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetDefaultApply", reflect.TypeOf((*MockRedisInterface)(nil).SetDefaultApply), arg0, arg1)
}

// SetDefaultLastRun mocks base method.
func (m *MockRedisInterface) SetDefaultLastRun(arg0 context.Context, arg1 *v1beta1.Run) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetDefaultLastRun", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetDefaultLastRun indicates an expected call of SetDefaultLastRun.
func (mr *MockRedisInterfaceMockRecorder) SetDefaultLastRun(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetDefaultLastRun", reflect.TypeOf((*MockRedisInterface)(nil).SetDefaultLastRun), arg0, arg1)
}

// SetPRRun mocks base method.
func (m *MockRedisInterface) SetPRRun(arg0 context.Context, arg1 *v1beta1.Run) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetPRRun", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetPRRun indicates an expected call of SetPRRun.
func (mr *MockRedisInterfaceMockRecorder) SetPRRun(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetPRRun", reflect.TypeOf((*MockRedisInterface)(nil).SetPRRun), arg0, arg1)
}

// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/utilitywarehouse/terraform-applier/git (interfaces: Repositories)

// Package git is a generated GoMock package.
package git

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
)

// MockRepositories is a mock of Repositories interface.
type MockRepositories struct {
	ctrl     *gomock.Controller
	recorder *MockRepositoriesMockRecorder
}

// MockRepositoriesMockRecorder is the mock recorder for MockRepositories.
type MockRepositoriesMockRecorder struct {
	mock *MockRepositories
}

// NewMockRepositories creates a new mock instance.
func NewMockRepositories(ctrl *gomock.Controller) *MockRepositories {
	mock := &MockRepositories{ctrl: ctrl}
	mock.recorder = &MockRepositoriesMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRepositories) EXPECT() *MockRepositoriesMockRecorder {
	return m.recorder
}

// ChangedFiles mocks base method.
func (m *MockRepositories) ChangedFiles(arg0 context.Context, arg1, arg2 string) ([]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ChangedFiles", arg0, arg1, arg2)
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ChangedFiles indicates an expected call of ChangedFiles.
func (mr *MockRepositoriesMockRecorder) ChangedFiles(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ChangedFiles", reflect.TypeOf((*MockRepositories)(nil).ChangedFiles), arg0, arg1, arg2)
}

// Clone mocks base method.
func (m *MockRepositories) Clone(arg0 context.Context, arg1, arg2, arg3, arg4 string, arg5 bool) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Clone", arg0, arg1, arg2, arg3, arg4, arg5)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Clone indicates an expected call of Clone.
func (mr *MockRepositoriesMockRecorder) Clone(arg0, arg1, arg2, arg3, arg4, arg5 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Clone", reflect.TypeOf((*MockRepositories)(nil).Clone), arg0, arg1, arg2, arg3, arg4, arg5)
}

// Hash mocks base method.
func (m *MockRepositories) Hash(arg0 context.Context, arg1, arg2, arg3 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Hash", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Hash indicates an expected call of Hash.
func (mr *MockRepositoriesMockRecorder) Hash(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Hash", reflect.TypeOf((*MockRepositories)(nil).Hash), arg0, arg1, arg2, arg3)
}

// LogMsg mocks base method.
func (m *MockRepositories) LogMsg(arg0 context.Context, arg1, arg2, arg3 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LogMsg", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LogMsg indicates an expected call of LogMsg.
func (mr *MockRepositoriesMockRecorder) LogMsg(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LogMsg", reflect.TypeOf((*MockRepositories)(nil).LogMsg), arg0, arg1, arg2, arg3)
}

// ObjectExists mocks base method.
func (m *MockRepositories) ObjectExists(arg0 context.Context, arg1, arg2 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ObjectExists", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// ObjectExists indicates an expected call of ObjectExists.
func (mr *MockRepositoriesMockRecorder) ObjectExists(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ObjectExists", reflect.TypeOf((*MockRepositories)(nil).ObjectExists), arg0, arg1, arg2)
}

// Subject mocks base method.
func (m *MockRepositories) Subject(arg0 context.Context, arg1, arg2 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Subject", arg0, arg1, arg2)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Subject indicates an expected call of Subject.
func (mr *MockRepositoriesMockRecorder) Subject(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Subject", reflect.TypeOf((*MockRepositories)(nil).Subject), arg0, arg1, arg2)
}

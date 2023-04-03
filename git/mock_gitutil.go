// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/utilitywarehouse/terraform-applier/git (interfaces: UtilInterface)

// Package git is a generated GoMock package.
package git

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
)

// MockUtilInterface is a mock of UtilInterface interface.
type MockUtilInterface struct {
	ctrl     *gomock.Controller
	recorder *MockUtilInterfaceMockRecorder
}

// MockUtilInterfaceMockRecorder is the mock recorder for MockUtilInterface.
type MockUtilInterfaceMockRecorder struct {
	mock *MockUtilInterface
}

// NewMockUtilInterface creates a new mock instance.
func NewMockUtilInterface(ctrl *gomock.Controller) *MockUtilInterface {
	mock := &MockUtilInterface{ctrl: ctrl}
	mock.recorder = &MockUtilInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockUtilInterface) EXPECT() *MockUtilInterfaceMockRecorder {
	return m.recorder
}

// HeadCommitHashAndLog mocks base method.
func (m *MockUtilInterface) HeadCommitHashAndLog(arg0 string) (string, string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HeadCommitHashAndLog", arg0)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(string)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// HeadCommitHashAndLog indicates an expected call of HeadCommitHashAndLog.
func (mr *MockUtilInterfaceMockRecorder) HeadCommitHashAndLog(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HeadCommitHashAndLog", reflect.TypeOf((*MockUtilInterface)(nil).HeadCommitHashAndLog), arg0)
}

// IsRepo mocks base method.
func (m *MockUtilInterface) IsRepo() (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsRepo")
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// IsRepo indicates an expected call of IsRepo.
func (mr *MockUtilInterfaceMockRecorder) IsRepo() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsRepo", reflect.TypeOf((*MockUtilInterface)(nil).IsRepo))
}

// RemoteURL mocks base method.
func (m *MockUtilInterface) RemoteURL() (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemoteURL")
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RemoteURL indicates an expected call of RemoteURL.
func (mr *MockUtilInterfaceMockRecorder) RemoteURL() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoteURL", reflect.TypeOf((*MockUtilInterface)(nil).RemoteURL))
}

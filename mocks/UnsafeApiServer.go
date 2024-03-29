// Code generated by mockery v2.14.0. DO NOT EDIT.

package mocks

import mock "github.com/stretchr/testify/mock"

// UnsafeApiServer is an autogenerated mock type for the UnsafeApiServer type
type UnsafeApiServer struct {
	mock.Mock
}

type UnsafeApiServer_Expecter struct {
	mock *mock.Mock
}

func (_m *UnsafeApiServer) EXPECT() *UnsafeApiServer_Expecter {
	return &UnsafeApiServer_Expecter{mock: &_m.Mock}
}

// mustEmbedUnimplementedApiServer provides a mock function with given fields:
func (_m *UnsafeApiServer) mustEmbedUnimplementedApiServer() {
	_m.Called()
}

// UnsafeApiServer_mustEmbedUnimplementedApiServer_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'mustEmbedUnimplementedApiServer'
type UnsafeApiServer_mustEmbedUnimplementedApiServer_Call struct {
	*mock.Call
}

// mustEmbedUnimplementedApiServer is a helper method to define mock.On call
func (_e *UnsafeApiServer_Expecter) mustEmbedUnimplementedApiServer() *UnsafeApiServer_mustEmbedUnimplementedApiServer_Call {
	return &UnsafeApiServer_mustEmbedUnimplementedApiServer_Call{Call: _e.mock.On("mustEmbedUnimplementedApiServer")}
}

func (_c *UnsafeApiServer_mustEmbedUnimplementedApiServer_Call) Run(run func()) *UnsafeApiServer_mustEmbedUnimplementedApiServer_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *UnsafeApiServer_mustEmbedUnimplementedApiServer_Call) Return() *UnsafeApiServer_mustEmbedUnimplementedApiServer_Call {
	_c.Call.Return()
	return _c
}

type mockConstructorTestingTNewUnsafeApiServer interface {
	mock.TestingT
	Cleanup(func())
}

// NewUnsafeApiServer creates a new instance of UnsafeApiServer. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewUnsafeApiServer(t mockConstructorTestingTNewUnsafeApiServer) *UnsafeApiServer {
	mock := &UnsafeApiServer{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}

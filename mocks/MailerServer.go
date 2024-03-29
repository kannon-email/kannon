// Code generated by mockery v2.14.0. DO NOT EDIT.

package mocks

import (
	context "context"

	apiv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"

	mock "github.com/stretchr/testify/mock"
)

// MailerServer is an autogenerated mock type for the MailerServer type
type MailerServer struct {
	mock.Mock
}

type MailerServer_Expecter struct {
	mock *mock.Mock
}

func (_m *MailerServer) EXPECT() *MailerServer_Expecter {
	return &MailerServer_Expecter{mock: &_m.Mock}
}

// SendHTML provides a mock function with given fields: _a0, _a1
func (_m *MailerServer) SendHTML(_a0 context.Context, _a1 *apiv1.SendHTMLReq) (*apiv1.SendRes, error) {
	ret := _m.Called(_a0, _a1)

	var r0 *apiv1.SendRes
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.SendHTMLReq) *apiv1.SendRes); ok {
		r0 = rf(_a0, _a1)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.SendRes)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.SendHTMLReq) error); ok {
		r1 = rf(_a0, _a1)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MailerServer_SendHTML_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'SendHTML'
type MailerServer_SendHTML_Call struct {
	*mock.Call
}

// SendHTML is a helper method to define mock.On call
//   - _a0 context.Context
//   - _a1 *apiv1.SendHTMLReq
func (_e *MailerServer_Expecter) SendHTML(_a0 interface{}, _a1 interface{}) *MailerServer_SendHTML_Call {
	return &MailerServer_SendHTML_Call{Call: _e.mock.On("SendHTML", _a0, _a1)}
}

func (_c *MailerServer_SendHTML_Call) Run(run func(_a0 context.Context, _a1 *apiv1.SendHTMLReq)) *MailerServer_SendHTML_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(*apiv1.SendHTMLReq))
	})
	return _c
}

func (_c *MailerServer_SendHTML_Call) Return(_a0 *apiv1.SendRes, _a1 error) *MailerServer_SendHTML_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

// SendTemplate provides a mock function with given fields: _a0, _a1
func (_m *MailerServer) SendTemplate(_a0 context.Context, _a1 *apiv1.SendTemplateReq) (*apiv1.SendRes, error) {
	ret := _m.Called(_a0, _a1)

	var r0 *apiv1.SendRes
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.SendTemplateReq) *apiv1.SendRes); ok {
		r0 = rf(_a0, _a1)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.SendRes)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.SendTemplateReq) error); ok {
		r1 = rf(_a0, _a1)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MailerServer_SendTemplate_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'SendTemplate'
type MailerServer_SendTemplate_Call struct {
	*mock.Call
}

// SendTemplate is a helper method to define mock.On call
//   - _a0 context.Context
//   - _a1 *apiv1.SendTemplateReq
func (_e *MailerServer_Expecter) SendTemplate(_a0 interface{}, _a1 interface{}) *MailerServer_SendTemplate_Call {
	return &MailerServer_SendTemplate_Call{Call: _e.mock.On("SendTemplate", _a0, _a1)}
}

func (_c *MailerServer_SendTemplate_Call) Run(run func(_a0 context.Context, _a1 *apiv1.SendTemplateReq)) *MailerServer_SendTemplate_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(*apiv1.SendTemplateReq))
	})
	return _c
}

func (_c *MailerServer_SendTemplate_Call) Return(_a0 *apiv1.SendRes, _a1 error) *MailerServer_SendTemplate_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

type mockConstructorTestingTNewMailerServer interface {
	mock.TestingT
	Cleanup(func())
}

// NewMailerServer creates a new instance of MailerServer. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewMailerServer(t mockConstructorTestingTNewMailerServer) *MailerServer {
	mock := &MailerServer{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}

// Code generated by mockery v2.14.0. DO NOT EDIT.

package mocks

import (
	smtp "github.com/ludusrusso/kannon/internal/smtp"
	mock "github.com/stretchr/testify/mock"
)

// Sender is an autogenerated mock type for the Sender type
type Sender struct {
	mock.Mock
}

type Sender_Expecter struct {
	mock *mock.Mock
}

func (_m *Sender) EXPECT() *Sender_Expecter {
	return &Sender_Expecter{mock: &_m.Mock}
}

// Send provides a mock function with given fields: from, to, msg
func (_m *Sender) Send(from string, to string, msg []byte) smtp.SenderError {
	ret := _m.Called(from, to, msg)

	var r0 smtp.SenderError
	if rf, ok := ret.Get(0).(func(string, string, []byte) smtp.SenderError); ok {
		r0 = rf(from, to, msg)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(smtp.SenderError)
		}
	}

	return r0
}

// Sender_Send_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Send'
type Sender_Send_Call struct {
	*mock.Call
}

// Send is a helper method to define mock.On call
//   - from string
//   - to string
//   - msg []byte
func (_e *Sender_Expecter) Send(from interface{}, to interface{}, msg interface{}) *Sender_Send_Call {
	return &Sender_Send_Call{Call: _e.mock.On("Send", from, to, msg)}
}

func (_c *Sender_Send_Call) Run(run func(from string, to string, msg []byte)) *Sender_Send_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string), args[1].(string), args[2].([]byte))
	})
	return _c
}

func (_c *Sender_Send_Call) Return(_a0 smtp.SenderError) *Sender_Send_Call {
	_c.Call.Return(_a0)
	return _c
}

// SenderName provides a mock function with given fields:
func (_m *Sender) SenderName() string {
	ret := _m.Called()

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	return r0
}

// Sender_SenderName_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'SenderName'
type Sender_SenderName_Call struct {
	*mock.Call
}

// SenderName is a helper method to define mock.On call
func (_e *Sender_Expecter) SenderName() *Sender_SenderName_Call {
	return &Sender_SenderName_Call{Call: _e.mock.On("SenderName")}
}

func (_c *Sender_SenderName_Call) Run(run func()) *Sender_SenderName_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *Sender_SenderName_Call) Return(_a0 string) *Sender_SenderName_Call {
	_c.Call.Return(_a0)
	return _c
}

type mockConstructorTestingTNewSender interface {
	mock.TestingT
	Cleanup(func())
}

// NewSender creates a new instance of Sender. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewSender(t mockConstructorTestingTNewSender) *Sender {
	mock := &Sender{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}

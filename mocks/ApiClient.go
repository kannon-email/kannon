// Code generated by mockery v2.14.0. DO NOT EDIT.

package mocks

import (
	context "context"

	apiv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"

	grpc "google.golang.org/grpc"

	mock "github.com/stretchr/testify/mock"
)

// ApiClient is an autogenerated mock type for the ApiClient type
type ApiClient struct {
	mock.Mock
}

type ApiClient_Expecter struct {
	mock *mock.Mock
}

func (_m *ApiClient) EXPECT() *ApiClient_Expecter {
	return &ApiClient_Expecter{mock: &_m.Mock}
}

// CreateDomain provides a mock function with given fields: ctx, in, opts
func (_m *ApiClient) CreateDomain(ctx context.Context, in *apiv1.CreateDomainRequest, opts ...grpc.CallOption) (*apiv1.Domain, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *apiv1.Domain
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.CreateDomainRequest, ...grpc.CallOption) *apiv1.Domain); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.Domain)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.CreateDomainRequest, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ApiClient_CreateDomain_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'CreateDomain'
type ApiClient_CreateDomain_Call struct {
	*mock.Call
}

// CreateDomain is a helper method to define mock.On call
//   - ctx context.Context
//   - in *apiv1.CreateDomainRequest
//   - opts ...grpc.CallOption
func (_e *ApiClient_Expecter) CreateDomain(ctx interface{}, in interface{}, opts ...interface{}) *ApiClient_CreateDomain_Call {
	return &ApiClient_CreateDomain_Call{Call: _e.mock.On("CreateDomain",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *ApiClient_CreateDomain_Call) Run(run func(ctx context.Context, in *apiv1.CreateDomainRequest, opts ...grpc.CallOption)) *ApiClient_CreateDomain_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*apiv1.CreateDomainRequest), variadicArgs...)
	})
	return _c
}

func (_c *ApiClient_CreateDomain_Call) Return(_a0 *apiv1.Domain, _a1 error) *ApiClient_CreateDomain_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

// CreateTemplate provides a mock function with given fields: ctx, in, opts
func (_m *ApiClient) CreateTemplate(ctx context.Context, in *apiv1.CreateTemplateReq, opts ...grpc.CallOption) (*apiv1.CreateTemplateRes, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *apiv1.CreateTemplateRes
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.CreateTemplateReq, ...grpc.CallOption) *apiv1.CreateTemplateRes); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.CreateTemplateRes)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.CreateTemplateReq, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ApiClient_CreateTemplate_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'CreateTemplate'
type ApiClient_CreateTemplate_Call struct {
	*mock.Call
}

// CreateTemplate is a helper method to define mock.On call
//   - ctx context.Context
//   - in *apiv1.CreateTemplateReq
//   - opts ...grpc.CallOption
func (_e *ApiClient_Expecter) CreateTemplate(ctx interface{}, in interface{}, opts ...interface{}) *ApiClient_CreateTemplate_Call {
	return &ApiClient_CreateTemplate_Call{Call: _e.mock.On("CreateTemplate",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *ApiClient_CreateTemplate_Call) Run(run func(ctx context.Context, in *apiv1.CreateTemplateReq, opts ...grpc.CallOption)) *ApiClient_CreateTemplate_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*apiv1.CreateTemplateReq), variadicArgs...)
	})
	return _c
}

func (_c *ApiClient_CreateTemplate_Call) Return(_a0 *apiv1.CreateTemplateRes, _a1 error) *ApiClient_CreateTemplate_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

// DeleteTemplate provides a mock function with given fields: ctx, in, opts
func (_m *ApiClient) DeleteTemplate(ctx context.Context, in *apiv1.DeleteTemplateReq, opts ...grpc.CallOption) (*apiv1.DeleteTemplateRes, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *apiv1.DeleteTemplateRes
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.DeleteTemplateReq, ...grpc.CallOption) *apiv1.DeleteTemplateRes); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.DeleteTemplateRes)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.DeleteTemplateReq, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ApiClient_DeleteTemplate_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'DeleteTemplate'
type ApiClient_DeleteTemplate_Call struct {
	*mock.Call
}

// DeleteTemplate is a helper method to define mock.On call
//   - ctx context.Context
//   - in *apiv1.DeleteTemplateReq
//   - opts ...grpc.CallOption
func (_e *ApiClient_Expecter) DeleteTemplate(ctx interface{}, in interface{}, opts ...interface{}) *ApiClient_DeleteTemplate_Call {
	return &ApiClient_DeleteTemplate_Call{Call: _e.mock.On("DeleteTemplate",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *ApiClient_DeleteTemplate_Call) Run(run func(ctx context.Context, in *apiv1.DeleteTemplateReq, opts ...grpc.CallOption)) *ApiClient_DeleteTemplate_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*apiv1.DeleteTemplateReq), variadicArgs...)
	})
	return _c
}

func (_c *ApiClient_DeleteTemplate_Call) Return(_a0 *apiv1.DeleteTemplateRes, _a1 error) *ApiClient_DeleteTemplate_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

// GetDomains provides a mock function with given fields: ctx, in, opts
func (_m *ApiClient) GetDomains(ctx context.Context, in *apiv1.GetDomainsReq, opts ...grpc.CallOption) (*apiv1.GetDomainsResponse, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *apiv1.GetDomainsResponse
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.GetDomainsReq, ...grpc.CallOption) *apiv1.GetDomainsResponse); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.GetDomainsResponse)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.GetDomainsReq, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ApiClient_GetDomains_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'GetDomains'
type ApiClient_GetDomains_Call struct {
	*mock.Call
}

// GetDomains is a helper method to define mock.On call
//   - ctx context.Context
//   - in *apiv1.GetDomainsReq
//   - opts ...grpc.CallOption
func (_e *ApiClient_Expecter) GetDomains(ctx interface{}, in interface{}, opts ...interface{}) *ApiClient_GetDomains_Call {
	return &ApiClient_GetDomains_Call{Call: _e.mock.On("GetDomains",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *ApiClient_GetDomains_Call) Run(run func(ctx context.Context, in *apiv1.GetDomainsReq, opts ...grpc.CallOption)) *ApiClient_GetDomains_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*apiv1.GetDomainsReq), variadicArgs...)
	})
	return _c
}

func (_c *ApiClient_GetDomains_Call) Return(_a0 *apiv1.GetDomainsResponse, _a1 error) *ApiClient_GetDomains_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

// GetTemplate provides a mock function with given fields: ctx, in, opts
func (_m *ApiClient) GetTemplate(ctx context.Context, in *apiv1.GetTemplateReq, opts ...grpc.CallOption) (*apiv1.GetTemplateRes, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *apiv1.GetTemplateRes
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.GetTemplateReq, ...grpc.CallOption) *apiv1.GetTemplateRes); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.GetTemplateRes)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.GetTemplateReq, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ApiClient_GetTemplate_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'GetTemplate'
type ApiClient_GetTemplate_Call struct {
	*mock.Call
}

// GetTemplate is a helper method to define mock.On call
//   - ctx context.Context
//   - in *apiv1.GetTemplateReq
//   - opts ...grpc.CallOption
func (_e *ApiClient_Expecter) GetTemplate(ctx interface{}, in interface{}, opts ...interface{}) *ApiClient_GetTemplate_Call {
	return &ApiClient_GetTemplate_Call{Call: _e.mock.On("GetTemplate",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *ApiClient_GetTemplate_Call) Run(run func(ctx context.Context, in *apiv1.GetTemplateReq, opts ...grpc.CallOption)) *ApiClient_GetTemplate_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*apiv1.GetTemplateReq), variadicArgs...)
	})
	return _c
}

func (_c *ApiClient_GetTemplate_Call) Return(_a0 *apiv1.GetTemplateRes, _a1 error) *ApiClient_GetTemplate_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

// GetTemplates provides a mock function with given fields: ctx, in, opts
func (_m *ApiClient) GetTemplates(ctx context.Context, in *apiv1.GetTemplatesReq, opts ...grpc.CallOption) (*apiv1.GetTemplatesRes, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *apiv1.GetTemplatesRes
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.GetTemplatesReq, ...grpc.CallOption) *apiv1.GetTemplatesRes); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.GetTemplatesRes)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.GetTemplatesReq, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ApiClient_GetTemplates_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'GetTemplates'
type ApiClient_GetTemplates_Call struct {
	*mock.Call
}

// GetTemplates is a helper method to define mock.On call
//   - ctx context.Context
//   - in *apiv1.GetTemplatesReq
//   - opts ...grpc.CallOption
func (_e *ApiClient_Expecter) GetTemplates(ctx interface{}, in interface{}, opts ...interface{}) *ApiClient_GetTemplates_Call {
	return &ApiClient_GetTemplates_Call{Call: _e.mock.On("GetTemplates",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *ApiClient_GetTemplates_Call) Run(run func(ctx context.Context, in *apiv1.GetTemplatesReq, opts ...grpc.CallOption)) *ApiClient_GetTemplates_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*apiv1.GetTemplatesReq), variadicArgs...)
	})
	return _c
}

func (_c *ApiClient_GetTemplates_Call) Return(_a0 *apiv1.GetTemplatesRes, _a1 error) *ApiClient_GetTemplates_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

// RegenerateDomainKey provides a mock function with given fields: ctx, in, opts
func (_m *ApiClient) RegenerateDomainKey(ctx context.Context, in *apiv1.RegenerateDomainKeyRequest, opts ...grpc.CallOption) (*apiv1.Domain, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *apiv1.Domain
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.RegenerateDomainKeyRequest, ...grpc.CallOption) *apiv1.Domain); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.Domain)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.RegenerateDomainKeyRequest, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ApiClient_RegenerateDomainKey_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'RegenerateDomainKey'
type ApiClient_RegenerateDomainKey_Call struct {
	*mock.Call
}

// RegenerateDomainKey is a helper method to define mock.On call
//   - ctx context.Context
//   - in *apiv1.RegenerateDomainKeyRequest
//   - opts ...grpc.CallOption
func (_e *ApiClient_Expecter) RegenerateDomainKey(ctx interface{}, in interface{}, opts ...interface{}) *ApiClient_RegenerateDomainKey_Call {
	return &ApiClient_RegenerateDomainKey_Call{Call: _e.mock.On("RegenerateDomainKey",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *ApiClient_RegenerateDomainKey_Call) Run(run func(ctx context.Context, in *apiv1.RegenerateDomainKeyRequest, opts ...grpc.CallOption)) *ApiClient_RegenerateDomainKey_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*apiv1.RegenerateDomainKeyRequest), variadicArgs...)
	})
	return _c
}

func (_c *ApiClient_RegenerateDomainKey_Call) Return(_a0 *apiv1.Domain, _a1 error) *ApiClient_RegenerateDomainKey_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

// UpdateTemplate provides a mock function with given fields: ctx, in, opts
func (_m *ApiClient) UpdateTemplate(ctx context.Context, in *apiv1.UpdateTemplateReq, opts ...grpc.CallOption) (*apiv1.UpdateTemplateRes, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *apiv1.UpdateTemplateRes
	if rf, ok := ret.Get(0).(func(context.Context, *apiv1.UpdateTemplateReq, ...grpc.CallOption) *apiv1.UpdateTemplateRes); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*apiv1.UpdateTemplateRes)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *apiv1.UpdateTemplateReq, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ApiClient_UpdateTemplate_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'UpdateTemplate'
type ApiClient_UpdateTemplate_Call struct {
	*mock.Call
}

// UpdateTemplate is a helper method to define mock.On call
//   - ctx context.Context
//   - in *apiv1.UpdateTemplateReq
//   - opts ...grpc.CallOption
func (_e *ApiClient_Expecter) UpdateTemplate(ctx interface{}, in interface{}, opts ...interface{}) *ApiClient_UpdateTemplate_Call {
	return &ApiClient_UpdateTemplate_Call{Call: _e.mock.On("UpdateTemplate",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *ApiClient_UpdateTemplate_Call) Run(run func(ctx context.Context, in *apiv1.UpdateTemplateReq, opts ...grpc.CallOption)) *ApiClient_UpdateTemplate_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*apiv1.UpdateTemplateReq), variadicArgs...)
	})
	return _c
}

func (_c *ApiClient_UpdateTemplate_Call) Return(_a0 *apiv1.UpdateTemplateRes, _a1 error) *ApiClient_UpdateTemplate_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

type mockConstructorTestingTNewApiClient interface {
	mock.TestingT
	Cleanup(func())
}

// NewApiClient creates a new instance of ApiClient. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewApiClient(t mockConstructorTestingTNewApiClient) *ApiClient {
	mock := &ApiClient{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package gateway

import (
	"context"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// Runtime contains the platform configuration and runner for one active binding.
type Runtime struct {
	Tenant  platform.Tenant
	App     platform.AgentApp
	Binding platform.ChannelBinding
	Runner  runner.Runner
}

// Validate checks that the runtime can process inbound messages.
func (r Runtime) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return err
	}
	if err := r.App.Validate(); err != nil {
		return err
	}
	if err := r.Binding.Validate(); err != nil {
		return err
	}
	if r.Runner == nil {
		return ErrRuntimeNotFound
	}
	if r.App.TenantID != r.Tenant.TenantID ||
		r.Binding.TenantID != r.Tenant.TenantID ||
		r.Binding.AppID != r.App.AppID {
		return ErrRuntimeMismatch
	}
	if r.Tenant.Status != "" && r.Tenant.Status != platform.TenantStatusActive {
		return ErrRuntimeInactive
	}
	if r.App.Status != "" && r.App.Status != platform.AppStatusActive {
		return ErrRuntimeInactive
	}
	if r.Binding.Status != "" && r.Binding.Status != platform.BindingStatusActive {
		return ErrRuntimeInactive
	}
	return nil
}

func (r Runtime) matchesInbound(msg platform.InboundMessage) bool {
	return r.Tenant.TenantID == msg.TenantID &&
		r.App.TenantID == msg.TenantID &&
		r.App.AppID == msg.AppID &&
		r.Binding.TenantID == msg.TenantID &&
		r.Binding.AppID == msg.AppID &&
		r.Binding.BindingID == msg.BindingID &&
		r.Binding.Channel == msg.Channel &&
		r.Binding.AccountID == msg.ChannelAccountID
}

// Registry resolves an inbound message to an active runtime.
type Registry interface {
	Lookup(ctx context.Context, msg platform.InboundMessage) (Runtime, bool, error)
}

// InMemoryRegistry stores runtimes by tenant, app, binding, channel, and account.
type InMemoryRegistry struct {
	mu       sync.RWMutex
	runtimes map[string]Runtime
}

// NewInMemoryRegistry creates an in-memory runtime registry.
func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{
		runtimes: make(map[string]Runtime),
	}
}

// Register stores one runtime.
func (r *InMemoryRegistry) Register(runtime Runtime) error {
	if err := runtime.Validate(); err != nil {
		return err
	}
	key := runtimeKey(
		runtime.Tenant.TenantID,
		runtime.App.AppID,
		runtime.Binding.BindingID,
		runtime.Binding.Channel,
		runtime.Binding.AccountID,
	)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimes[key] = runtime
	return nil
}

// Lookup returns the runtime for an inbound message.
func (r *InMemoryRegistry) Lookup(
	ctx context.Context,
	msg platform.InboundMessage,
) (Runtime, bool, error) {
	if err := ctx.Err(); err != nil {
		return Runtime{}, false, err
	}
	key := runtimeKey(
		msg.TenantID,
		msg.AppID,
		msg.BindingID,
		msg.Channel,
		msg.ChannelAccountID,
	)
	r.mu.RLock()
	defer r.mu.RUnlock()
	runtime, ok := r.runtimes[key]
	return runtime, ok, nil
}

func runtimeKey(tenantID, appID, bindingID, channel, accountID string) string {
	return platform.IdempotencyKey(
		tenantID+"|"+appID,
		channel,
		accountID,
		bindingID,
	)
}

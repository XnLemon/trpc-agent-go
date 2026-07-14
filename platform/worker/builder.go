//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package worker

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/artifact"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/platform/gateway"
	"trpc.group/trpc-go/trpc-agent-go/platform/storagerouter"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// AgentDependencies contains tenant-scoped services available while building
// one runtime agent.
type AgentDependencies struct {
	Tenant    platform.Tenant
	App       platform.AgentApp
	Binding   platform.ChannelBinding
	Storage   storagerouter.StorageAdapter
	Session   session.Service
	Memory    memory.Service
	Artifact  artifact.Service
	Knowledge knowledge.Knowledge
	Audit     platform.AuditSink
	// ToolPolicy is the tenant app policy used to build the agent tool surface.
	ToolPolicy platform.ToolPolicy
	// ToolFilter narrows tools visible to the model for this runtime.
	ToolFilter tool.FilterFunc
	// ToolPermissionPolicy enforces tool-call authorization before execution.
	ToolPermissionPolicy tool.PermissionPolicy
}

// AgentFactory builds an agent for one tenant app runtime.
type AgentFactory interface {
	BuildAgent(ctx context.Context, dependencies AgentDependencies) (agent.Agent, error)
}

// AgentFactoryFunc adapts a function into an AgentFactory.
type AgentFactoryFunc func(context.Context, AgentDependencies) (agent.Agent, error)

// BuildAgent implements AgentFactory.
func (f AgentFactoryFunc) BuildAgent(
	ctx context.Context,
	dependencies AgentDependencies,
) (agent.Agent, error) {
	return f(ctx, dependencies)
}

// RuntimeBuilder assembles gateway runtimes from tenant storage profiles.
type RuntimeBuilder struct {
	router             storagerouter.Router
	factory            AgentFactory
	toolPolicyProvider ToolPolicyProvider
}

type runtimeServices struct {
	storage   storagerouter.StorageAdapter
	session   session.Service
	memory    memory.Service
	artifact  artifact.Service
	knowledge knowledge.Knowledge
	audit     platform.AuditSink
}

// RuntimeBuilderOption configures RuntimeBuilder.
type RuntimeBuilderOption func(*RuntimeBuilder)

// WithToolPolicyProvider resolves configured app tool policies.
func WithToolPolicyProvider(provider ToolPolicyProvider) RuntimeBuilderOption {
	return func(builder *RuntimeBuilder) {
		builder.toolPolicyProvider = provider
	}
}

// NewRuntimeBuilder creates a runtime builder.
func NewRuntimeBuilder(
	router storagerouter.Router,
	factory AgentFactory,
) (*RuntimeBuilder, error) {
	return NewRuntimeBuilderWithOptions(router, factory)
}

// NewRuntimeBuilderWithOptions creates a runtime builder with options.
func NewRuntimeBuilderWithOptions(
	router storagerouter.Router,
	factory AgentFactory,
	opts ...RuntimeBuilderOption,
) (*RuntimeBuilder, error) {
	if isNilDependency(router) {
		return nil, ErrStorageRouterRequired
	}
	if isNilDependency(factory) {
		return nil, ErrAgentFactoryRequired
	}
	builder := &RuntimeBuilder{
		router:  router,
		factory: factory,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(builder)
		}
	}
	return builder, nil
}

// Build resolves tenant-scoped storage services, builds the configured agent,
// and injects Session, Memory, and Artifact services into a Runner.
func (b *RuntimeBuilder) Build(
	ctx context.Context,
	tenant platform.Tenant,
	app platform.AgentApp,
	binding platform.ChannelBinding,
) (gateway.Runtime, error) {
	if err := ctx.Err(); err != nil {
		return gateway.Runtime{}, err
	}
	if err := validateRuntimeConfig(tenant, app, binding); err != nil {
		return gateway.Runtime{}, err
	}
	resolvedPolicy, err := b.resolveToolPolicyConfig(ctx, tenant, app)
	if err != nil {
		return gateway.Runtime{}, err
	}

	services, err := b.resolveRuntimeServices(ctx, tenant, app)
	if err != nil {
		return gateway.Runtime{}, err
	}
	permissionPolicy, err := compileToolPolicy(resolvedPolicy, services.audit)
	if err != nil {
		return gateway.Runtime{}, err
	}
	var toolFilter tool.FilterFunc
	if permissionPolicy != nil {
		toolFilter = permissionPolicy.ToolFilter()
	}

	dependencies := AgentDependencies{
		Tenant:               tenant,
		App:                  app,
		Binding:              binding,
		Storage:              services.storage,
		Session:              services.session,
		Memory:               services.memory,
		Artifact:             services.artifact,
		Knowledge:            services.knowledge,
		Audit:                services.audit,
		ToolPolicy:           resolvedPolicy,
		ToolFilter:           toolFilter,
		ToolPermissionPolicy: permissionPolicy,
	}
	ag, err := b.factory.BuildAgent(ctx, dependencies)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("build agent: %w", err)
	}
	if isNilDependency(ag) {
		return gateway.Runtime{}, ErrAgentRequired
	}
	if ag.Info().Name != app.AgentName {
		return gateway.Runtime{}, ErrAgentNameMismatch
	}

	runtime := gateway.Runtime{
		Tenant:  tenant,
		App:     app,
		Binding: binding,
		Runner: runner.NewRunner(
			services.storage.Scope().ScopedAppName(app.AppID),
			ag,
			runner.WithSessionService(services.session),
			runner.WithMemoryService(services.memory),
			runner.WithArtifactService(services.artifact),
		),
		Audit:                services.audit,
		ToolFilter:           toolFilter,
		ToolPermissionPolicy: permissionPolicy,
	}
	if err := runtime.Validate(); err != nil {
		_ = runtime.Runner.Close()
		return gateway.Runtime{}, err
	}
	return runtime, nil
}

func (b *RuntimeBuilder) resolveRuntimeServices(
	ctx context.Context,
	tenant platform.Tenant,
	app platform.AgentApp,
) (runtimeServices, error) {
	adapterRouter, ok := b.router.(storagerouter.AdapterRouter)
	if !ok {
		return runtimeServices{}, fmt.Errorf("resolve storage adapter: %w", ErrStorageAdapterRequired)
	}
	storage, err := adapterRouter.Adapter(ctx, tenant.TenantID, app.StorageProfileID)
	if err != nil {
		return runtimeServices{}, fmt.Errorf("resolve storage adapter: %w", err)
	}
	if isNilDependency(storage) {
		return runtimeServices{}, fmt.Errorf("resolve storage adapter: %w", ErrStorageAdapterRequired)
	}
	sessionService, err := storage.Session(ctx)
	if err != nil {
		return runtimeServices{}, fmt.Errorf("resolve session service: %w", err)
	}
	if isNilDependency(sessionService) {
		return runtimeServices{}, fmt.Errorf("resolve session service: %w", ErrSessionServiceRequired)
	}
	memoryService, err := storage.Memory(ctx)
	if err != nil {
		return runtimeServices{}, fmt.Errorf("resolve memory service: %w", err)
	}
	if isNilDependency(memoryService) {
		return runtimeServices{}, fmt.Errorf("resolve memory service: %w", ErrMemoryServiceRequired)
	}
	artifactService, err := storage.Artifact(ctx)
	if err != nil {
		return runtimeServices{}, fmt.Errorf("resolve artifact service: %w", err)
	}
	if isNilDependency(artifactService) {
		return runtimeServices{}, fmt.Errorf("resolve artifact service: %w", ErrArtifactServiceRequired)
	}
	knowledgeService, err := storage.Knowledge(ctx)
	if err != nil {
		return runtimeServices{}, fmt.Errorf("resolve knowledge service: %w", err)
	}
	if isNilDependency(knowledgeService) {
		return runtimeServices{}, fmt.Errorf("resolve knowledge service: %w", ErrKnowledgeServiceRequired)
	}
	auditSink, err := storage.Audit(ctx)
	if err != nil {
		return runtimeServices{}, fmt.Errorf("resolve audit sink: %w", err)
	}
	if isNilDependency(auditSink) {
		return runtimeServices{}, fmt.Errorf("resolve audit sink: %w", ErrAuditSinkRequired)
	}
	return runtimeServices{
		storage:   storage,
		session:   sessionService,
		memory:    memoryService,
		artifact:  artifactService,
		knowledge: knowledgeService,
		audit:     auditSink,
	}, nil
}

func validateRuntimeConfig(
	tenant platform.Tenant,
	app platform.AgentApp,
	binding platform.ChannelBinding,
) error {
	if err := tenant.Validate(); err != nil {
		return err
	}
	if err := app.Validate(); err != nil {
		return err
	}
	if err := binding.Validate(); err != nil {
		return err
	}
	if app.TenantID != tenant.TenantID ||
		binding.TenantID != tenant.TenantID ||
		binding.AppID != app.AppID {
		return ErrRuntimeIdentityMismatch
	}
	if tenant.Status != "" && tenant.Status != platform.TenantStatusActive {
		return gateway.ErrRuntimeInactive
	}
	if app.Status != "" && app.Status != platform.AppStatusActive {
		return gateway.ErrRuntimeInactive
	}
	if binding.Status != "" && binding.Status != platform.BindingStatusActive {
		return gateway.ErrRuntimeInactive
	}
	if strings.TrimSpace(app.AppName) == "" {
		return ErrAppNameRequired
	}
	if strings.TrimSpace(app.AgentName) == "" {
		return ErrAgentNameRequired
	}
	if strings.TrimSpace(app.StorageProfileID) == "" {
		return ErrStorageProfileIDRequired
	}
	return nil
}

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.Map,
		reflect.Pointer,
		reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

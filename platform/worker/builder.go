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
	router  storagerouter.Router
	factory AgentFactory
}

// NewRuntimeBuilder creates a runtime builder.
func NewRuntimeBuilder(
	router storagerouter.Router,
	factory AgentFactory,
) (*RuntimeBuilder, error) {
	if isNilDependency(router) {
		return nil, ErrStorageRouterRequired
	}
	if isNilDependency(factory) {
		return nil, ErrAgentFactoryRequired
	}
	return &RuntimeBuilder{
		router:  router,
		factory: factory,
	}, nil
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

	storage, err := b.router.Adapter(ctx, tenant.TenantID, app.StorageProfileID)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve storage adapter: %w", err)
	}
	if isNilDependency(storage) {
		return gateway.Runtime{}, fmt.Errorf("resolve storage adapter: %w", ErrStorageAdapterRequired)
	}
	sessionService, err := storage.Session(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve session service: %w", err)
	}
	if isNilDependency(sessionService) {
		return gateway.Runtime{}, fmt.Errorf("resolve session service: %w", ErrSessionServiceRequired)
	}
	memoryService, err := storage.Memory(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve memory service: %w", err)
	}
	if isNilDependency(memoryService) {
		return gateway.Runtime{}, fmt.Errorf("resolve memory service: %w", ErrMemoryServiceRequired)
	}
	artifactService, err := storage.Artifact(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve artifact service: %w", err)
	}
	if isNilDependency(artifactService) {
		return gateway.Runtime{}, fmt.Errorf("resolve artifact service: %w", ErrArtifactServiceRequired)
	}
	knowledgeService, err := storage.Knowledge(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve knowledge service: %w", err)
	}
	if isNilDependency(knowledgeService) {
		return gateway.Runtime{}, fmt.Errorf("resolve knowledge service: %w", ErrKnowledgeServiceRequired)
	}
	auditSink, err := storage.Audit(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve audit sink: %w", err)
	}
	if isNilDependency(auditSink) {
		return gateway.Runtime{}, fmt.Errorf("resolve audit sink: %w", ErrAuditSinkRequired)
	}

	dependencies := AgentDependencies{
		Tenant:    tenant,
		App:       app,
		Binding:   binding,
		Storage:   storage,
		Session:   sessionService,
		Memory:    memoryService,
		Artifact:  artifactService,
		Knowledge: knowledgeService,
		Audit:     auditSink,
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
			storage.Scope().ScopedAppName(app.AppID),
			ag,
			runner.WithSessionService(sessionService),
			runner.WithMemoryService(memoryService),
			runner.WithArtifactService(artifactService),
		),
		Audit: auditSink,
	}
	if err := runtime.Validate(); err != nil {
		_ = runtime.Runner.Close()
		return gateway.Runtime{}, err
	}
	return runtime, nil
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

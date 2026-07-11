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
	"trpc.group/trpc-go/trpc-agent-go/platform/toolpolicy"
	"trpc.group/trpc-go/trpc-agent-go/plugin"
	"trpc.group/trpc-go/trpc-agent-go/plugin/guardrail/approval"
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
	// Plugins contains runner-scoped plugins assembled by worker governance.
	Plugins []plugin.Plugin
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

	storage, err := b.router.Adapter(ctx, tenant.TenantID, app.StorageProfileID)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve storage adapter: %w", err)
	}
	sessionService, err := storage.Session(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve session service: %w", err)
	}
	memoryService, err := storage.Memory(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve memory service: %w", err)
	}
	artifactService, err := storage.Artifact(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve artifact service: %w", err)
	}
	knowledgeService, err := storage.Knowledge(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve knowledge service: %w", err)
	}
	auditSink, err := storage.Audit(ctx)
	if err != nil {
		return gateway.Runtime{}, fmt.Errorf("resolve audit sink: %w", err)
	}
	permissionPolicy, err := compileToolPolicy(resolvedPolicy, auditSink)
	if err != nil {
		return gateway.Runtime{}, err
	}
	var toolFilter tool.FilterFunc
	if permissionPolicy != nil {
		toolFilter = permissionPolicy.ToolFilter()
	}
	plugins, err := buildToolGovernancePlugins(resolvedPolicy, auditSink)
	if err != nil {
		return gateway.Runtime{}, err
	}

	dependencies := AgentDependencies{
		Tenant:               tenant,
		App:                  app,
		Binding:              binding,
		Storage:              storage,
		Session:              sessionService,
		Memory:               memoryService,
		Artifact:             artifactService,
		Knowledge:            knowledgeService,
		Audit:                auditSink,
		ToolPolicy:           resolvedPolicy,
		ToolFilter:           toolFilter,
		ToolPermissionPolicy: permissionPolicy,
		Plugins:              plugins,
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

	runnerOptions := []runner.Option{
		runner.WithSessionService(sessionService),
		runner.WithMemoryService(memoryService),
		runner.WithArtifactService(artifactService),
	}
	if len(plugins) > 0 {
		runnerOptions = append(runnerOptions, runner.WithPlugins(plugins...))
	}

	runtime := gateway.Runtime{
		Tenant:  tenant,
		App:     app,
		Binding: binding,
		Runner: runner.NewRunner(
			storage.Scope().ScopedAppName(app.AppID),
			ag,
			runnerOptions...,
		),
		Audit:                auditSink,
		ToolFilter:           toolFilter,
		ToolPermissionPolicy: permissionPolicy,
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

func buildToolGovernancePlugins(
	policy platform.ToolPolicy,
	auditSink platform.AuditSink,
) ([]plugin.Plugin, error) {
	if strings.TrimSpace(policy.PolicyID) == "" {
		return nil, nil
	}
	opts, approvalRequired := toolApprovalOptions(policy)
	if !approvalRequired {
		return nil, nil
	}
	reviewer, err := toolpolicy.NewReviewer(policy)
	if err != nil {
		return nil, fmt.Errorf("build tool approval reviewer: %w", err)
	}
	opts = append(
		opts,
		approval.WithReviewer(reviewer),
		approval.WithAuditSink(auditSink),
		approval.WithApproverUserID("platform-tool-policy-reviewer"),
	)
	approvalPlugin, err := approval.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("build tool approval plugin: %w", err)
	}
	return []plugin.Plugin{approvalPlugin}, nil
}

func toolApprovalOptions(policy platform.ToolPolicy) ([]approval.Option, bool) {
	defaultPolicy := approval.ToolPolicySkipApproval
	if len(normalizedToolNames(policy.ToolWhitelist)) > 0 {
		defaultPolicy = approval.ToolPolicyDenied
	}
	opts := []approval.Option{
		approval.WithDefaultToolPolicy(defaultPolicy),
	}
	if policy.DangerousToolAction != platform.DangerousToolActionAsk {
		return opts, false
	}
	opts = append(opts, approval.WithMetadataRiskPolicy(approval.ToolPolicyRequireApproval))
	whitelist := normalizedToolNames(policy.ToolWhitelist)
	denied := normalizedToolNames(policy.ToolDenylist, policy.PlatformDenylist)
	hasWhitelist := len(whitelist) > 0
	approvalRequired := true
	for _, name := range whitelist {
		opts = append(opts, approval.WithToolPolicy(name, approval.ToolPolicySkipApproval))
	}
	for _, name := range normalizedToolNames(policy.HighRiskTools) {
		if hasWhitelist && !containsToolName(whitelist, name) {
			continue
		}
		if containsToolName(denied, name) {
			continue
		}
		opts = append(opts, approval.WithToolPolicy(name, approval.ToolPolicyRequireApproval))
		approvalRequired = true
	}
	for _, name := range denied {
		opts = append(opts, approval.WithToolPolicy(name, approval.ToolPolicyDenied))
	}
	return opts, approvalRequired
}

func normalizedToolNames(lists ...[]string) []string {
	seen := make(map[string]struct{})
	var names []string
	for _, list := range lists {
		for _, raw := range list {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}

func containsToolName(names []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, name := range names {
		if name == target {
			return true
		}
	}
	return false
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

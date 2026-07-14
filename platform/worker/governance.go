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
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/platform/toolpolicy"
)

// ToolPolicyProvider resolves a configured tenant app tool policy.
type ToolPolicyProvider interface {
	ResolveToolPolicy(
		ctx context.Context,
		tenantID string,
		appID string,
		policyID string,
	) (platform.ToolPolicy, error)
}

// ToolPolicyProviderFunc adapts a function into a ToolPolicyProvider.
type ToolPolicyProviderFunc func(
	context.Context,
	string,
	string,
	string,
) (platform.ToolPolicy, error)

// ResolveToolPolicy implements ToolPolicyProvider.
func (f ToolPolicyProviderFunc) ResolveToolPolicy(
	ctx context.Context,
	tenantID string,
	appID string,
	policyID string,
) (platform.ToolPolicy, error) {
	return f(ctx, tenantID, appID, policyID)
}

func (b *RuntimeBuilder) resolveToolPolicyConfig(
	ctx context.Context,
	tenant platform.Tenant,
	app platform.AgentApp,
) (platform.ToolPolicy, error) {
	policyID := strings.TrimSpace(app.ToolPolicyID)
	if policyID == "" {
		return platform.ToolPolicy{}, nil
	}
	if isNilDependency(b.toolPolicyProvider) {
		return platform.ToolPolicy{}, ErrToolPolicyProviderRequired
	}
	policy, err := b.toolPolicyProvider.ResolveToolPolicy(
		ctx,
		tenant.TenantID,
		app.AppID,
		policyID,
	)
	if err != nil {
		return platform.ToolPolicy{}, fmt.Errorf("resolve tool policy: %w", err)
	}
	if policy.TenantID != tenant.TenantID ||
		policy.AppID != app.AppID ||
		policy.PolicyID != policyID {
		return platform.ToolPolicy{}, ErrToolPolicyIdentityMismatch
	}
	return cloneToolPolicy(policy), nil
}

func compileToolPolicy(
	policy platform.ToolPolicy,
	auditSink platform.AuditSink,
) (*toolpolicy.Policy, error) {
	if strings.TrimSpace(policy.PolicyID) == "" {
		return nil, nil
	}
	compiled, err := toolpolicy.New(
		policy,
		toolpolicy.WithAuditSink(auditSink),
	)
	if err != nil {
		return nil, fmt.Errorf("compile tool policy: %w", err)
	}
	return compiled, nil
}

func cloneToolPolicy(policy platform.ToolPolicy) platform.ToolPolicy {
	policy.ToolWhitelist = append([]string(nil), policy.ToolWhitelist...)
	policy.ToolDenylist = append([]string(nil), policy.ToolDenylist...)
	policy.ArgumentRedactionRules = append(
		[]string(nil),
		policy.ArgumentRedactionRules...,
	)
	policy.PlatformDenylist = append([]string(nil), policy.PlatformDenylist...)
	policy.HighRiskTools = append([]string(nil), policy.HighRiskTools...)
	return policy
}

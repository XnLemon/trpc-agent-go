//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package toolpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/plugin"
	"trpc.group/trpc-go/trpc-agent-go/plugin/guardrail/approval"
	"trpc.group/trpc-go/trpc-agent-go/plugin/guardrail/approval/review"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Policy adapts a platform.ToolPolicy to tool.PermissionPolicy.
type Policy struct {
	name     string
	policy   platform.ToolPolicy
	audit    platform.AuditSink
	redactor *platform.Redactor
	now      func() time.Time
}

// Option configures Policy.
type Option func(*Policy)

// WithName sets the plugin name used when Policy is registered.
func WithName(name string) Option {
	return func(p *Policy) {
		name = strings.TrimSpace(name)
		if name != "" {
			p.name = name
		}
	}
}

// WithAuditSink records each non-allow decision to audit.
func WithAuditSink(sink platform.AuditSink) Option {
	return func(p *Policy) {
		p.audit = sink
	}
}

// WithRedactor sets the redactor used before writing tool arguments to audit.
func WithRedactor(redactor *platform.Redactor) Option {
	return func(p *Policy) {
		if redactor != nil {
			p.redactor = redactor
		}
	}
}

// WithNow sets the clock used for audit records.
func WithNow(now func() time.Time) Option {
	return func(p *Policy) {
		if now != nil {
			p.now = now
		}
	}
}

// New creates a runtime permission policy from platform tool governance.
func New(policy platform.ToolPolicy, opts ...Option) (*Policy, error) {
	if err := validate(policy); err != nil {
		return nil, err
	}
	redactor, err := platform.NewRedactor(policy.ArgumentRedactionRules...)
	if err != nil {
		return nil, fmt.Errorf("newing platform tool policy: redaction rules: %w", err)
	}
	p := &Policy{
		name:     "platform_tool_policy",
		policy:   policy,
		redactor: redactor,
		now:      time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p, nil
}

// Name implements plugin.Plugin when Policy is registered with a plugin manager.
func (p *Policy) Name() string {
	if p == nil || p.name == "" {
		return "platform_tool_policy"
	}
	return p.name
}

// Register adds the policy as a before-tool callback for name-based governance.
// Use CheckToolPermission as a per-run tool.PermissionPolicy when decisions
// must include tool metadata such as destructive, read-only, or open-world.
func (p *Policy) Register(r *plugin.Registry) {
	if p == nil || r == nil {
		return
	}
	r.BeforeTool(p.beforeTool())
}

// CheckToolPermission implements tool.PermissionPolicy.
func (p *Policy) CheckToolPermission(
	ctx context.Context,
	req *tool.PermissionRequest,
) (tool.PermissionDecision, error) {
	if req == nil {
		return tool.AllowPermission(), nil
	}
	name := strings.TrimSpace(req.ToolName)
	if name == "" && req.Declaration != nil {
		name = strings.TrimSpace(req.Declaration.Name)
	}
	decision, reason, audit := p.decide(req, name)
	if audit {
		p.writeAudit(ctx, req, name, string(decision.Action), reason)
	}
	return decision, nil
}

func (p *Policy) beforeTool() tool.BeforeToolCallbackStructured {
	return func(ctx context.Context, args *tool.BeforeToolArgs) (*tool.BeforeToolResult, error) {
		if args == nil {
			return nil, nil
		}
		req := &tool.PermissionRequest{
			ToolName:    args.ToolName,
			ToolCallID:  args.ToolCallID,
			Declaration: args.Declaration,
			Arguments:   args.Arguments,
		}
		decision, reason, audit := p.decideNameOnly(req, req.ToolName)
		if audit {
			p.writeAudit(ctx, req, req.ToolName, string(decision.Action), reason)
		}
		var err error
		if err != nil {
			return nil, err
		}
		decision, err = tool.NormalizePermissionDecision(decision)
		if err != nil {
			return nil, err
		}
		if decision.Action == tool.PermissionActionAllow {
			return nil, nil
		}
		return &tool.BeforeToolResult{
			CustomResult: tool.PermissionResultFor(req.ToolName, decision),
		}, nil
	}
}

// ApprovalOptions maps name-based parts of the platform policy into approval
// plugin options. A non-empty whitelist remains a hard boundary. Use Policy as
// tool.PermissionPolicy when metadata-based high-risk decisions and
// allow_with_audit records are required.
func ApprovalOptions(policy platform.ToolPolicy) ([]approval.Option, error) {
	if err := validate(policy); err != nil {
		return nil, err
	}
	defaultPolicy := approval.ToolPolicySkipApproval
	if len(normalizedList(policy.ToolWhitelist)) > 0 ||
		policy.DangerousToolAction == platform.DangerousToolActionDeny {
		defaultPolicy = approval.ToolPolicyDenied
	}
	opts := []approval.Option{approval.WithDefaultToolPolicy(defaultPolicy)}
	whitelist := normalizedList(policy.ToolWhitelist)
	hasWhitelist := len(whitelist) > 0
	for _, name := range whitelist {
		opts = append(opts, approval.WithToolPolicy(name, approval.ToolPolicySkipApproval))
	}
	for _, name := range normalizedList(policy.HighRiskTools) {
		if hasWhitelist && !contains(whitelist, name) {
			continue
		}
		switch policy.DangerousToolAction {
		case platform.DangerousToolActionDeny:
			opts = append(opts, approval.WithToolPolicy(name, approval.ToolPolicyDenied))
		case platform.DangerousToolActionAsk:
			opts = append(opts, approval.WithToolPolicy(name, approval.ToolPolicyRequireApproval))
		case platform.DangerousToolActionAllowWithAudit, "":
			opts = append(opts, approval.WithToolPolicy(name, approval.ToolPolicySkipApproval))
		}
	}
	for _, name := range normalizedList(policy.ToolDenylist, policy.PlatformDenylist) {
		opts = append(opts, approval.WithToolPolicy(name, approval.ToolPolicyDenied))
	}
	return opts, nil
}

// Reviewer wraps Policy as an approval reviewer for name-boundary checks. It
// rejects denylisted and non-whitelisted tools, but treats ask/approval-required
// decisions as reviewer-approved so the approval plugin can own that flow.
type Reviewer struct {
	policy *Policy
}

// NewReviewer creates an approval reviewer backed by platform tool governance.
func NewReviewer(policy platform.ToolPolicy, opts ...Option) (*Reviewer, error) {
	p, err := New(policy, opts...)
	if err != nil {
		return nil, err
	}
	return &Reviewer{policy: p}, nil
}

// Review implements approval/review.Reviewer.
func (r *Reviewer) Review(ctx context.Context, req *review.Request) (*review.Decision, error) {
	if r == nil || r.policy == nil || req == nil {
		return &review.Decision{Approved: true}, nil
	}
	permissionReq := &tool.PermissionRequest{
		ToolName:    req.Action.ToolName,
		Declaration: &tool.Declaration{Name: req.Action.ToolName, Description: req.Action.ToolDescription},
		Arguments:   req.Action.Arguments,
	}
	decision, reason, audit := r.policy.decideReviewer(permissionReq, permissionReq.ToolName)
	if audit {
		r.policy.writeAudit(ctx, permissionReq, permissionReq.ToolName, string(decision.Action), reason)
	}
	var err error
	decision, err = tool.NormalizePermissionDecision(decision)
	if err != nil {
		return nil, err
	}
	return &review.Decision{
		Approved:  decision.Action != tool.PermissionActionDeny,
		RiskLevel: string(decision.Action),
		Reason:    decision.Reason,
	}, nil
}

func (p *Policy) decide(req *tool.PermissionRequest, name string) (tool.PermissionDecision, string, bool) {
	if contains(policyDenylist(p.policy), name) {
		reason := fmt.Sprintf("tool %q is denied by platform tool policy", name)
		return tool.DenyPermission(reason), reason, true
	}
	if len(normalizedList(p.policy.ToolWhitelist)) > 0 &&
		!contains(normalizedList(p.policy.ToolWhitelist), name) {
		reason := fmt.Sprintf("tool %q is not in platform tool whitelist", name)
		return tool.DenyPermission(reason), reason, true
	}
	if isHighRisk(p.policy, req, name) {
		switch p.policy.DangerousToolAction {
		case platform.DangerousToolActionDeny:
			reason := fmt.Sprintf("high-risk tool %q is denied by platform tool policy", name)
			return tool.DenyPermission(reason), reason, true
		case platform.DangerousToolActionAsk:
			reason := fmt.Sprintf("high-risk tool %q requires approval by platform tool policy", name)
			return tool.AskPermission(reason), reason, true
		case platform.DangerousToolActionAllowWithAudit, "":
			reason := fmt.Sprintf("high-risk tool %q allowed with audit by platform tool policy", name)
			return tool.AllowPermission(), reason, true
		}
	}
	return tool.AllowPermission(), "", false
}

func (p *Policy) decideNameOnly(req *tool.PermissionRequest, name string) (tool.PermissionDecision, string, bool) {
	if contains(policyDenylist(p.policy), name) {
		reason := fmt.Sprintf("tool %q is denied by platform tool policy", name)
		return tool.DenyPermission(reason), reason, true
	}
	if len(normalizedList(p.policy.ToolWhitelist)) > 0 &&
		!contains(normalizedList(p.policy.ToolWhitelist), name) {
		reason := fmt.Sprintf("tool %q is not in platform tool whitelist", name)
		return tool.DenyPermission(reason), reason, true
	}
	if contains(normalizedList(p.policy.HighRiskTools), name) {
		switch p.policy.DangerousToolAction {
		case platform.DangerousToolActionDeny:
			reason := fmt.Sprintf("high-risk tool %q is denied by platform tool policy", name)
			return tool.DenyPermission(reason), reason, true
		case platform.DangerousToolActionAsk:
			reason := fmt.Sprintf("high-risk tool %q requires approval by platform tool policy", name)
			return tool.AskPermission(reason), reason, true
		case platform.DangerousToolActionAllowWithAudit, "":
			reason := fmt.Sprintf("high-risk tool %q allowed with audit by platform tool policy", name)
			return tool.AllowPermission(), reason, true
		}
	}
	if req != nil && req.Metadata != (tool.ToolMetadata{}) {
		return p.decide(req, name)
	}
	return tool.AllowPermission(), "", false
}

func (p *Policy) decideReviewer(req *tool.PermissionRequest, name string) (tool.PermissionDecision, string, bool) {
	if contains(policyDenylist(p.policy), name) {
		reason := fmt.Sprintf("tool %q is denied by platform tool policy", name)
		return tool.DenyPermission(reason), reason, true
	}
	if len(normalizedList(p.policy.ToolWhitelist)) > 0 &&
		!contains(normalizedList(p.policy.ToolWhitelist), name) {
		reason := fmt.Sprintf("tool %q is not in platform tool whitelist", name)
		return tool.DenyPermission(reason), reason, true
	}
	if contains(normalizedList(p.policy.HighRiskTools), name) {
		switch p.policy.DangerousToolAction {
		case platform.DangerousToolActionDeny:
			reason := fmt.Sprintf("high-risk tool %q is denied by platform tool policy", name)
			return tool.DenyPermission(reason), reason, true
		case platform.DangerousToolActionAsk:
			reason := fmt.Sprintf("high-risk tool %q approved by platform approval reviewer", name)
			return tool.AllowPermission(), reason, true
		case platform.DangerousToolActionAllowWithAudit, "":
			reason := fmt.Sprintf("high-risk tool %q allowed with audit by platform tool policy", name)
			return tool.AllowPermission(), reason, true
		}
	}
	if req != nil && req.Metadata != (tool.ToolMetadata{}) {
		return p.decide(req, name)
	}
	return tool.AllowPermission(), "", false
}

func validate(policy platform.ToolPolicy) error {
	switch policy.DangerousToolAction {
	case "", platform.DangerousToolActionDeny,
		platform.DangerousToolActionAsk,
		platform.DangerousToolActionAllowWithAudit:
		return nil
	default:
		return fmt.Errorf("invalid dangerous tool action %q", policy.DangerousToolAction)
	}
}

func isHighRisk(policy platform.ToolPolicy, req *tool.PermissionRequest, name string) bool {
	if contains(normalizedList(policy.HighRiskTools), name) {
		return true
	}
	if req == nil {
		return false
	}
	return req.Metadata.Destructive || !req.Metadata.ReadOnly || req.Metadata.OpenWorld
}

func policyDenylist(policy platform.ToolPolicy) []string {
	return normalizedList(policy.ToolDenylist, policy.PlatformDenylist)
}

func normalizedList(lists ...[]string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, list := range lists {
		for _, item := range list {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func (p *Policy) writeAudit(
	ctx context.Context,
	req *tool.PermissionRequest,
	toolName string,
	decision string,
	reason string,
) {
	if p.audit == nil {
		return
	}
	argsSummary := argumentSummary(req.Arguments)
	_ = p.audit.WriteAudit(ctx, platform.AuditRecord{
		AuditID:           platform.AuditID(p.policy.TenantID, p.policy.AppID, toolName, req.ToolCallID, decision, argsSummary),
		TenantID:          p.policy.TenantID,
		AppID:             p.policy.AppID,
		ToolName:          toolName,
		Decision:          decision,
		DecisionReason:    reason,
		RedactedDetailRef: argsSummary,
		RedactionVersion:  "platform-toolpolicy-v1",
		CreatedAt:         p.now(),
	})
}

func argumentSummary(args []byte) string {
	if len(args) == 0 {
		return ""
	}
	sum := sha256.Sum256(args)
	return fmt.Sprintf("sha256:%s bytes:%d", hex.EncodeToString(sum[:]), len(args))
}

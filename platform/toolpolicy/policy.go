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
	"regexp"
	"strconv"
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

// ApprovalSummary is the safe approval-facing summary of one tool call.
type ApprovalSummary struct {
	TenantID         string
	AppID            string
	PolicyID         string
	ToolName         string
	ToolCallID       string
	Decision         tool.PermissionAction
	Reason           string
	ArgumentsDigest  string
	ArgumentsBytes   int
	RequiresApproval bool
	ReadOnly         bool
	Destructive      bool
	OpenWorld        bool
	ConcurrencySafe  bool
	SearchOrRead     bool
	MaxResultSize    int
	RedactionVersion string
	CreatedAt        time.Time
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

// WithRedactor overrides the configured redactor. Approval summaries still
// store only argument digests and never include raw tool arguments.
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
	if err := validateRuntimeIdentity(policy); err != nil {
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
		summary, err := p.ApprovalSummary(req, decision, reason)
		if err != nil {
			return tool.PermissionDecision{}, err
		}
		if err := p.writeAudit(ctx, summary); err != nil {
			return tool.PermissionDecision{}, err
		}
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
			summary, err := p.ApprovalSummary(req, decision, reason)
			if err != nil {
				return nil, err
			}
			if err := p.writeAudit(ctx, summary); err != nil {
				return nil, err
			}
		}
		decision, err := tool.NormalizePermissionDecision(decision)
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
		summary, err := r.policy.ApprovalSummary(permissionReq, decision, reason)
		if err != nil {
			return nil, err
		}
		if err := r.policy.writeAudit(ctx, summary); err != nil {
			return nil, err
		}
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

func validateRuntimeIdentity(policy platform.ToolPolicy) error {
	if strings.TrimSpace(policy.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(policy.AppID) == "" {
		return fmt.Errorf("app_id is required")
	}
	if strings.TrimSpace(policy.PolicyID) == "" {
		return fmt.Errorf("policy_id is required")
	}
	return nil
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

// ApprovalSummary builds a redacted approval summary suitable for audit,
// approval messages, and logs. Raw tool arguments are never included.
func (p *Policy) ApprovalSummary(
	req *tool.PermissionRequest,
	decision tool.PermissionDecision,
	reason string,
) (ApprovalSummary, error) {
	if p == nil {
		return ApprovalSummary{}, fmt.Errorf("policy is nil")
	}
	if req == nil {
		return ApprovalSummary{}, fmt.Errorf("permission request is nil")
	}
	name := strings.TrimSpace(req.ToolName)
	if name == "" && req.Declaration != nil {
		name = strings.TrimSpace(req.Declaration.Name)
	}
	if name == "" {
		return ApprovalSummary{}, fmt.Errorf("tool_name is required")
	}
	decision, err := tool.NormalizePermissionDecision(decision)
	if err != nil {
		return ApprovalSummary{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = strings.TrimSpace(decision.Reason)
	}
	if err := platformSafeText("tool_name", name); err != nil {
		return ApprovalSummary{}, err
	}
	if err := platformSafeText("tool_call_id", req.ToolCallID); err != nil {
		return ApprovalSummary{}, err
	}
	if err := platformSafeText("reason", reason); err != nil {
		return ApprovalSummary{}, err
	}
	argumentsDigest, argumentsBytes := argumentDigest(req.Arguments)
	summary := ApprovalSummary{
		TenantID:         strings.TrimSpace(p.policy.TenantID),
		AppID:            strings.TrimSpace(p.policy.AppID),
		PolicyID:         strings.TrimSpace(p.policy.PolicyID),
		ToolName:         name,
		ToolCallID:       strings.TrimSpace(req.ToolCallID),
		Decision:         decision.Action,
		Reason:           reason,
		ArgumentsDigest:  argumentsDigest,
		ArgumentsBytes:   argumentsBytes,
		RequiresApproval: decision.Action == tool.PermissionActionAsk,
		ReadOnly:         req.Metadata.ReadOnly,
		Destructive:      req.Metadata.Destructive,
		OpenWorld:        req.Metadata.OpenWorld,
		ConcurrencySafe:  req.Metadata.ConcurrencySafe,
		SearchOrRead:     req.Metadata.SearchOrRead,
		MaxResultSize:    req.Metadata.MaxResultSize,
		RedactionVersion: "platform-toolpolicy-v1",
		CreatedAt:        p.now(),
	}
	if err := summary.Validate(); err != nil {
		return ApprovalSummary{}, err
	}
	return summary, nil
}

// Validate checks that the summary is safe to expose outside the tool runtime.
func (s ApprovalSummary) Validate() error {
	if err := s.validateIdentity(); err != nil {
		return err
	}
	if err := s.validateArguments(); err != nil {
		return err
	}
	if err := s.validateDecision(); err != nil {
		return err
	}
	if strings.TrimSpace(s.RedactionVersion) == "" {
		return fmt.Errorf("redaction_version is required")
	}
	if s.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	return platformSafeText("detail_ref", s.DetailRef())
}

func (s ApprovalSummary) validateIdentity() error {
	if strings.TrimSpace(s.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(s.AppID) == "" {
		return fmt.Errorf("app_id is required")
	}
	if strings.TrimSpace(s.PolicyID) == "" {
		return fmt.Errorf("policy_id is required")
	}
	if err := platformSafeText("tenant_id", s.TenantID); err != nil {
		return err
	}
	if err := platformSafeText("app_id", s.AppID); err != nil {
		return err
	}
	if err := platformSafeText("policy_id", s.PolicyID); err != nil {
		return err
	}
	if strings.TrimSpace(s.ToolName) == "" {
		return fmt.Errorf("tool_name is required")
	}
	if err := platformSafeText("tool_name", s.ToolName); err != nil {
		return err
	}
	if err := platformSafeText("tool_call_id", s.ToolCallID); err != nil {
		return err
	}
	if err := platformSafeText("reason", s.Reason); err != nil {
		return err
	}
	return nil
}

func (s ApprovalSummary) validateArguments() error {
	if s.ArgumentsBytes < 0 {
		return fmt.Errorf("arguments_bytes must be greater than or equal to 0")
	}
	if s.ArgumentsBytes == 0 {
		if s.ArgumentsDigest != "" {
			return fmt.Errorf("arguments_digest must be empty when arguments_bytes is 0")
		}
	} else if !validSHA256Digest(s.ArgumentsDigest) {
		return fmt.Errorf("arguments_digest must be sha256 followed by a 64 character hex digest")
	}
	if s.MaxResultSize < 0 {
		return fmt.Errorf("max_result_size must be greater than or equal to 0")
	}
	return nil
}

func (s ApprovalSummary) validateDecision() error {
	switch s.Decision {
	case tool.PermissionActionAllow:
		if s.RequiresApproval {
			return fmt.Errorf("requires_approval must be false for allow decisions")
		}
	case tool.PermissionActionDeny:
		if s.RequiresApproval {
			return fmt.Errorf("requires_approval must be false for deny decisions")
		}
	case tool.PermissionActionAsk:
		if !s.RequiresApproval {
			return fmt.Errorf("requires_approval must be true for ask decisions")
		}
	case "":
		return fmt.Errorf("decision is required")
	default:
		return fmt.Errorf("invalid decision %q", s.Decision)
	}
	return nil
}

func (p *Policy) writeAudit(ctx context.Context, summary ApprovalSummary) error {
	if p.audit == nil {
		return nil
	}
	detailRef := summary.DetailRef()
	if err := p.audit.WriteAudit(ctx, platform.AuditRecord{
		AuditID:           platform.AuditID(summary.TenantID, summary.AppID, summary.ToolName, summary.ToolCallID, string(summary.Decision), detailRef),
		TenantID:          summary.TenantID,
		AppID:             summary.AppID,
		ToolName:          summary.ToolName,
		Decision:          string(summary.Decision),
		DecisionReason:    summary.Reason,
		RedactedDetailRef: detailRef,
		RedactionVersion:  summary.RedactionVersion,
		CreatedAt:         summary.CreatedAt,
	}); err != nil {
		return fmt.Errorf("write tool policy audit: %w", err)
	}
	return nil
}

// DetailRef returns compact non-secret detail that can be stored in audit logs.
func (s ApprovalSummary) DetailRef() string {
	parts := []string{
		"tool:" + s.ToolName,
		"decision:" + string(s.Decision),
	}
	if s.ToolCallID != "" {
		parts = append(parts, "tool_call_id:"+s.ToolCallID)
	}
	if s.ArgumentsDigest != "" {
		parts = append(parts, "args:"+s.ArgumentsDigest)
		parts = append(parts, "args_bytes:"+strconv.Itoa(s.ArgumentsBytes))
	}
	if s.RequiresApproval {
		parts = append(parts, "requires_approval:true")
	}
	if s.ReadOnly {
		parts = append(parts, "read_only:true")
	}
	if s.Destructive {
		parts = append(parts, "destructive:true")
	}
	if s.OpenWorld {
		parts = append(parts, "open_world:true")
	}
	return strings.Join(parts, " ")
}

func argumentDigest(args []byte) (string, int) {
	if len(args) == 0 {
		return "", 0
	}
	sum := sha256.Sum256(args)
	return "sha256:" + hex.EncodeToString(sum[:]), len(args)
}

func argumentSummary(args []byte) string {
	digest, size := argumentDigest(args)
	if digest == "" {
		return ""
	}
	return fmt.Sprintf("%s bytes:%d", digest, size)
}

var sha256DigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func validSHA256Digest(value string) bool {
	return sha256DigestPattern.MatchString(value)
}

func platformSafeText(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	redactor, err := platform.NewRedactor()
	if err != nil {
		return fmt.Errorf("%s: redactor unavailable: %w", field, err)
	}
	if redactor.Redact(value) != value {
		return fmt.Errorf("%s contains unredacted sensitive content", field)
	}
	return nil
}

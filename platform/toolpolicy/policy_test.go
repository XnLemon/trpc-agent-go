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
	"errors"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/plugin"
	"trpc.group/trpc-go/trpc-agent-go/plugin/guardrail/approval"
	"trpc.group/trpc-go/trpc-agent-go/plugin/guardrail/approval/review"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestPolicyDeniesToolOutsideWhitelist(t *testing.T) {
	p := newPolicy(t, platform.ToolPolicy{
		TenantID:      "tenant",
		AppID:         "app",
		ToolWhitelist: []string{"knowledge_search"},
	})

	decision, err := p.CheckToolPermission(context.Background(), request("shell", tool.ToolMetadata{}))
	if err != nil {
		t.Fatalf("CheckToolPermission: %v", err)
	}
	if decision.Action != tool.PermissionActionDeny {
		t.Fatalf("expected deny, got %+v", decision)
	}
	if !strings.Contains(decision.Reason, "whitelist") {
		t.Fatalf("expected whitelist reason, got %q", decision.Reason)
	}
}

func TestPolicyDenylistOverridesWhitelist(t *testing.T) {
	p := newPolicy(t, platform.ToolPolicy{
		ToolWhitelist: []string{"shell"},
		ToolDenylist:  []string{"shell"},
	})

	decision, err := p.CheckToolPermission(context.Background(), request("shell", tool.ToolMetadata{}))
	if err != nil {
		t.Fatalf("CheckToolPermission: %v", err)
	}
	if decision.Action != tool.PermissionActionDeny {
		t.Fatalf("expected deny, got %+v", decision)
	}
	if !strings.Contains(decision.Reason, "denied") {
		t.Fatalf("expected denied reason, got %q", decision.Reason)
	}
}

func TestPolicyAsksForHighRiskTool(t *testing.T) {
	p := newPolicy(t, platform.ToolPolicy{
		DangerousToolAction: platform.DangerousToolActionAsk,
		HighRiskTools:       []string{"workspace_write"},
	})

	decision, err := p.CheckToolPermission(context.Background(), request("workspace_write", tool.ToolMetadata{}))
	if err != nil {
		t.Fatalf("CheckToolPermission: %v", err)
	}
	if decision.Action != tool.PermissionActionAsk {
		t.Fatalf("expected ask, got %+v", decision)
	}
}

func TestPolicyAllowsHighRiskWithAuditAndRedactsArguments(t *testing.T) {
	audit := platform.NewInMemoryAuditSink()
	now := time.Unix(100, 0)
	p := newPolicy(
		t,
		platform.ToolPolicy{
			TenantID:            "tenant",
			AppID:               "app",
			DangerousToolAction: platform.DangerousToolActionAllowWithAudit,
			HighRiskTools:       []string{"http_post"},
		},
		WithAuditSink(audit),
		WithNow(func() time.Time { return now }),
	)

	decision, err := p.CheckToolPermission(
		context.Background(),
		request("http_post", tool.ToolMetadata{}, []byte(`{"Authorization":"Bearer raw-token","url":"https://example.com"}`)),
	)
	if err != nil {
		t.Fatalf("CheckToolPermission: %v", err)
	}
	if decision.Action != tool.PermissionActionAllow {
		t.Fatalf("expected allow, got %+v", decision)
	}
	records := audit.Records()
	if len(records) != 1 {
		t.Fatalf("expected one audit record, got %d", len(records))
	}
	record := records[0]
	if record.Decision != string(tool.PermissionActionAllow) ||
		record.AuditID == "" ||
		record.ToolName != "http_post" ||
		record.TenantID != "tenant" ||
		record.AppID != "app" ||
		!record.CreatedAt.Equal(now) {
		t.Fatalf("unexpected audit record: %+v", record)
	}
	if strings.Contains(record.RedactedDetailRef, "raw-token") {
		t.Fatalf("audit leaked raw token: %q", record.RedactedDetailRef)
	}
	if strings.Contains(record.RedactedDetailRef, "example.com") ||
		strings.Contains(record.RedactedDetailRef, "Authorization") {
		t.Fatalf("audit leaked raw argument content: %q", record.RedactedDetailRef)
	}
	if !strings.Contains(record.RedactedDetailRef, "args:sha256:") ||
		!strings.Contains(record.RedactedDetailRef, "args_bytes:") ||
		!strings.Contains(record.RedactedDetailRef, "decision:allow") {
		t.Fatalf("expected safe detail summary, got %q", record.RedactedDetailRef)
	}
}

func TestPolicyBuildsApprovalSummaryWithoutRawArguments(t *testing.T) {
	now := time.Unix(200, 0)
	p := newPolicy(
		t,
		platform.ToolPolicy{
			TenantID:            "tenant",
			AppID:               "app",
			PolicyID:            "policy",
			DangerousToolAction: platform.DangerousToolActionAsk,
			HighRiskTools:       []string{"workspace_write"},
		},
		WithNow(func() time.Time { return now }),
	)
	req := request(
		"workspace_write",
		tool.ToolMetadata{
			Destructive:     true,
			OpenWorld:       true,
			ConcurrencySafe: false,
			MaxResultSize:   4096,
		},
		[]byte(`{"path":"/private/file","api_key":"sk-secret"}`),
	)
	req.ToolCallID = "call-1"

	decision, err := p.CheckToolPermission(context.Background(), req)
	if err != nil {
		t.Fatalf("CheckToolPermission: %v", err)
	}
	summary, err := p.ApprovalSummary(req, decision, decision.Reason)
	if err != nil {
		t.Fatalf("ApprovalSummary: %v", err)
	}
	if summary.TenantID != "tenant" ||
		summary.AppID != "app" ||
		summary.PolicyID != "policy" ||
		summary.ToolName != "workspace_write" ||
		summary.ToolCallID != "call-1" ||
		summary.Decision != tool.PermissionActionAsk ||
		!summary.RequiresApproval ||
		!summary.Destructive ||
		!summary.OpenWorld ||
		summary.MaxResultSize != 4096 ||
		!summary.CreatedAt.Equal(now) {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.ArgumentsBytes == 0 || !strings.HasPrefix(summary.ArgumentsDigest, "sha256:") {
		t.Fatalf("expected argument digest, got %+v", summary)
	}
	detail := summary.DetailRef()
	if strings.Contains(detail, "sk-secret") ||
		strings.Contains(detail, "/private/file") ||
		strings.Contains(detail, "api_key") {
		t.Fatalf("summary detail leaked raw arguments: %q", detail)
	}
	if !strings.Contains(detail, "requires_approval:true") ||
		!strings.Contains(detail, "destructive:true") ||
		!strings.Contains(detail, "open_world:true") {
		t.Fatalf("summary detail missing risk markers: %q", detail)
	}
}

func TestApprovalSummaryValidationRejectsUnsafeOrInconsistentFields(t *testing.T) {
	now := time.Unix(300, 0)
	valid := ApprovalSummary{
		TenantID:         "tenant",
		AppID:            "app",
		PolicyID:         "policy",
		ToolName:         "workspace_write",
		ToolCallID:       "call-1",
		Decision:         tool.PermissionActionAsk,
		Reason:           "high-risk tool requires approval",
		ArgumentsDigest:  "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		ArgumentsBytes:   16,
		RequiresApproval: true,
		RedactionVersion: "platform-toolpolicy-v1",
		CreatedAt:        now,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate valid summary: %v", err)
	}

	unsafe := valid
	unsafe.Reason = "sk-secret-token"
	if err := unsafe.Validate(); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("expected unsafe reason rejection, got %v", err)
	}

	unsafeToolName := valid
	unsafeToolName.ToolName = "api_key: sk-secret-token"
	if err := unsafeToolName.Validate(); err == nil || !strings.Contains(err.Error(), "tool_name") {
		t.Fatalf("expected unsafe tool name rejection, got %v", err)
	}

	unsafeToolCallID := valid
	unsafeToolCallID.ToolCallID = "token=sk-secret-token"
	if err := unsafeToolCallID.Validate(); err == nil || !strings.Contains(err.Error(), "tool_call_id") {
		t.Fatalf("expected unsafe tool call id rejection, got %v", err)
	}

	wrongApproval := valid
	wrongApproval.RequiresApproval = false
	if err := wrongApproval.Validate(); err == nil || !strings.Contains(err.Error(), "requires_approval") {
		t.Fatalf("expected ask approval invariant, got %v", err)
	}

	wrongDigest := valid
	wrongDigest.ArgumentsDigest = "raw-json"
	if err := wrongDigest.Validate(); err == nil || !strings.Contains(err.Error(), "arguments_digest") {
		t.Fatalf("expected digest prefix rejection, got %v", err)
	}

	unsafeDigest := valid
	unsafeDigest.ArgumentsDigest = "sha256:sk-secret-token"
	if err := unsafeDigest.Validate(); err == nil || !strings.Contains(err.Error(), "arguments_digest") {
		t.Fatalf("expected digest hex rejection, got %v", err)
	}

	noArguments := valid
	noArguments.ArgumentsBytes = 0
	if err := noArguments.Validate(); err == nil || !strings.Contains(err.Error(), "arguments_digest") {
		t.Fatalf("expected empty-arguments digest rejection, got %v", err)
	}

	allowNeedsApproval := valid
	allowNeedsApproval.Decision = tool.PermissionActionAllow
	if err := allowNeedsApproval.Validate(); err == nil || !strings.Contains(err.Error(), "requires_approval") {
		t.Fatalf("expected allow approval invariant, got %v", err)
	}
}

func TestPolicyAuditIDUsesToolCallBoundary(t *testing.T) {
	audit := platform.NewInMemoryAuditSink()
	p := newPolicy(
		t,
		platform.ToolPolicy{
			TenantID:            "tenant",
			AppID:               "app",
			DangerousToolAction: platform.DangerousToolActionAllowWithAudit,
			HighRiskTools:       []string{"http_post"},
		},
		WithAuditSink(audit),
	)
	args := []byte(`{"url":"https://example.com"}`)
	req1 := request("http_post", tool.ToolMetadata{}, args)
	req1.ToolCallID = "call-1"
	req2 := request("http_post", tool.ToolMetadata{}, args)
	req2.ToolCallID = "call-2"

	if _, err := p.CheckToolPermission(context.Background(), req1); err != nil {
		t.Fatalf("CheckToolPermission req1: %v", err)
	}
	if _, err := p.CheckToolPermission(context.Background(), req2); err != nil {
		t.Fatalf("CheckToolPermission req2: %v", err)
	}
	records := audit.Records()
	if len(records) != 2 {
		t.Fatalf("expected two audit records, got %d", len(records))
	}
	if records[0].AuditID == records[1].AuditID {
		t.Fatalf("expected tool-call-scoped audit ids, got %q", records[0].AuditID)
	}

	retryAudit := platform.NewInMemoryAuditSink()
	retryPolicy := newPolicy(
		t,
		platform.ToolPolicy{
			TenantID:            "tenant",
			AppID:               "app",
			DangerousToolAction: platform.DangerousToolActionAllowWithAudit,
			HighRiskTools:       []string{"http_post"},
		},
		WithAuditSink(retryAudit),
	)
	retryReq := request("http_post", tool.ToolMetadata{}, args)
	retryReq.ToolCallID = "call-1"
	if _, err := retryPolicy.CheckToolPermission(context.Background(), retryReq); err != nil {
		t.Fatalf("CheckToolPermission retry: %v", err)
	}
	retryRecords := retryAudit.Records()
	if len(retryRecords) != 1 {
		t.Fatalf("expected one retry audit record, got %d", len(retryRecords))
	}
	if records[0].AuditID != retryRecords[0].AuditID {
		t.Fatalf("expected stable audit id for same tool call, got %q and %q", records[0].AuditID, retryRecords[0].AuditID)
	}
}

func TestPolicyNilRedactorStillDoesNotLeakArguments(t *testing.T) {
	audit := platform.NewInMemoryAuditSink()
	p := newPolicy(
		t,
		platform.ToolPolicy{
			TenantID:            "tenant",
			AppID:               "app",
			DangerousToolAction: platform.DangerousToolActionAllowWithAudit,
			HighRiskTools:       []string{"http_post"},
		},
		WithAuditSink(audit),
		WithRedactor(nil),
	)

	_, err := p.CheckToolPermission(
		context.Background(),
		request("http_post", tool.ToolMetadata{}, []byte(`{"email":"person@example.com","path":"/private/file"}`)),
	)
	if err != nil {
		t.Fatalf("CheckToolPermission: %v", err)
	}
	records := audit.Records()
	if len(records) != 1 {
		t.Fatalf("expected one audit record, got %d", len(records))
	}
	if records[0].AuditID == "" {
		t.Fatalf("expected audit id")
	}
	if strings.Contains(records[0].RedactedDetailRef, "person@example.com") ||
		strings.Contains(records[0].RedactedDetailRef, "/private/file") {
		t.Fatalf("audit leaked raw argument content: %q", records[0].RedactedDetailRef)
	}
}

func TestPolicyDeniesDestructiveMetadata(t *testing.T) {
	p := newPolicy(t, platform.ToolPolicy{
		DangerousToolAction: platform.DangerousToolActionDeny,
	})

	decision, err := p.CheckToolPermission(
		context.Background(),
		request("shell", tool.ToolMetadata{Destructive: true}),
	)
	if err != nil {
		t.Fatalf("CheckToolPermission: %v", err)
	}
	if decision.Action != tool.PermissionActionDeny {
		t.Fatalf("expected deny, got %+v", decision)
	}
}

func TestPolicyRejectsInvalidDangerousAction(t *testing.T) {
	_, err := New(defaultPolicy(platform.ToolPolicy{
		DangerousToolAction: platform.DangerousToolAction("bad"),
	}))
	if err == nil {
		t.Fatalf("expected invalid action to fail")
	}
}

func TestPolicyRejectsMissingRuntimeIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy platform.ToolPolicy
		want   string
	}{
		{
			name:   "tenant",
			policy: platform.ToolPolicy{AppID: "app", PolicyID: "policy"},
			want:   "tenant_id",
		},
		{
			name:   "app",
			policy: platform.ToolPolicy{TenantID: "tenant", PolicyID: "policy"},
			want:   "app_id",
		},
		{
			name:   "policy",
			policy: platform.ToolPolicy{TenantID: "tenant", AppID: "app"},
			want:   "policy_id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.policy); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s validation error, got %v", tc.want, err)
			}
		})
	}
}

func TestPolicyReturnsAuditSinkErrors(t *testing.T) {
	p := newPolicy(
		t,
		platform.ToolPolicy{
			DangerousToolAction: platform.DangerousToolActionAllowWithAudit,
			HighRiskTools:       []string{"http_post"},
		},
		WithAuditSink(failingAuditSink{}),
	)

	_, err := p.CheckToolPermission(
		context.Background(),
		request("http_post", tool.ToolMetadata{}, []byte(`{"url":"https://example.com"}`)),
	)
	if err == nil || !strings.Contains(err.Error(), "write tool policy audit") {
		t.Fatalf("expected audit sink error, got %v", err)
	}
}

func TestApprovalOptionsMapPolicy(t *testing.T) {
	opts, err := ApprovalOptions(platform.ToolPolicy{
		ToolWhitelist:       []string{"search", "shell"},
		ToolDenylist:        []string{"shell"},
		PlatformDenylist:    []string{"admin_delete"},
		DangerousToolAction: platform.DangerousToolActionAsk,
		HighRiskTools:       []string{"workspace_write"},
	})
	if err != nil {
		t.Fatalf("ApprovalOptions: %v", err)
	}
	p, err := approval.New(append(opts, approval.WithReviewer(allowReviewer{}))...)
	if err != nil {
		t.Fatalf("approval.New: %v", err)
	}
	callbacks := plugin.MustNewManager(p).ToolCallbacks()

	denied, err := callbacks.RunBeforeTool(context.Background(), &tool.BeforeToolArgs{ToolName: "shell"})
	if err != nil {
		t.Fatalf("RunBeforeTool deny: %v", err)
	}
	if denied == nil || denied.CustomResult == nil {
		t.Fatalf("expected shell to be denied")
	}
	approvalRequired, err := callbacks.RunBeforeTool(context.Background(), &tool.BeforeToolArgs{ToolName: "workspace_write"})
	if err != nil {
		t.Fatalf("RunBeforeTool approval: %v", err)
	}
	if approvalRequired == nil || approvalRequired.CustomResult == nil {
		t.Fatalf("expected high-risk tool outside whitelist to be denied")
	}
	skipped, err := callbacks.RunBeforeTool(context.Background(), &tool.BeforeToolArgs{ToolName: "search"})
	if err != nil {
		t.Fatalf("RunBeforeTool skip: %v", err)
	}
	if skipped != nil {
		t.Fatalf("expected whitelisted search to skip approval, got %+v", skipped)
	}
	outsideWhitelist, err := callbacks.RunBeforeTool(context.Background(), &tool.BeforeToolArgs{ToolName: "unlisted"})
	if err != nil {
		t.Fatalf("RunBeforeTool outside whitelist: %v", err)
	}
	if outsideWhitelist == nil || outsideWhitelist.CustomResult == nil {
		t.Fatalf("expected non-whitelisted tool to be denied")
	}
}

func TestPolicyRegisterAppliesNameBasedGovernance(t *testing.T) {
	audit := platform.NewInMemoryAuditSink()
	p := newPolicy(t, platform.ToolPolicy{
		TenantID:            "tenant",
		AppID:               "app",
		DangerousToolAction: platform.DangerousToolActionAsk,
		HighRiskTools:       []string{"workspace_write"},
	}, WithAuditSink(audit))
	manager := plugin.MustNewManager(p)
	callbacks := manager.ToolCallbacks()

	result, err := callbacks.RunBeforeTool(context.Background(), &tool.BeforeToolArgs{
		ToolName:  "workspace_write",
		Arguments: []byte(`{"path":"/private/file"}`),
	})
	if err != nil {
		t.Fatalf("RunBeforeTool: %v", err)
	}
	if result == nil || result.CustomResult == nil {
		t.Fatalf("expected approval-required result")
	}
	if len(audit.Records()) != 1 || audit.Records()[0].AuditID == "" {
		t.Fatalf("expected audit record with id, got %+v", audit.Records())
	}
	permissionResult, ok := result.CustomResult.(tool.PermissionResult)
	if !ok {
		t.Fatalf("expected tool.PermissionResult, got %T", result.CustomResult)
	}
	if permissionResult.Status != tool.PermissionResultStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %+v", permissionResult)
	}
	if strings.Contains(audit.Records()[0].RedactedDetailRef, "/private/file") {
		t.Fatalf("audit leaked raw argument content: %q", audit.Records()[0].RedactedDetailRef)
	}
}

func TestPolicyRegisterDoesNotTreatUnknownMetadataAsHighRisk(t *testing.T) {
	p := newPolicy(t, platform.ToolPolicy{
		DangerousToolAction: platform.DangerousToolActionAsk,
	})
	manager := plugin.MustNewManager(p)
	callbacks := manager.ToolCallbacks()

	result, err := callbacks.RunBeforeTool(context.Background(), &tool.BeforeToolArgs{
		ToolName: "read_tool",
	})
	if err != nil {
		t.Fatalf("RunBeforeTool: %v", err)
	}
	if result != nil {
		t.Fatalf("expected unknown metadata in register path to continue, got %+v", result)
	}
}

func TestReviewerMapsPolicyDecisionToApprovalDecision(t *testing.T) {
	reviewer, err := NewReviewer(defaultPolicy(platform.ToolPolicy{
		ToolWhitelist: []string{"search"},
	}))
	if err != nil {
		t.Fatalf("NewReviewer: %v", err)
	}

	decision, err := reviewer.Review(context.Background(), &review.Request{
		Action: review.Action{ToolName: "shell"},
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if decision.Approved {
		t.Fatalf("expected shell outside whitelist to be rejected")
	}
}

func TestReviewerApprovesAskDecisionForApprovalPluginFlow(t *testing.T) {
	reviewer, err := NewReviewer(defaultPolicy(platform.ToolPolicy{
		DangerousToolAction: platform.DangerousToolActionAsk,
		HighRiskTools:       []string{"workspace_write"},
	}))
	if err != nil {
		t.Fatalf("NewReviewer: %v", err)
	}

	decision, err := reviewer.Review(context.Background(), &review.Request{
		Action: review.Action{ToolName: "workspace_write"},
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if !decision.Approved {
		t.Fatalf("expected ask decision to be approved inside approval flow, got %+v", decision)
	}
}

func TestApprovalOptionsWithReviewerAllowsWhitelistedHighRiskAsk(t *testing.T) {
	policy := defaultPolicy(platform.ToolPolicy{
		ToolWhitelist:       []string{"workspace_write"},
		DangerousToolAction: platform.DangerousToolActionAsk,
		HighRiskTools:       []string{"workspace_write"},
	})
	reviewer, err := NewReviewer(policy)
	if err != nil {
		t.Fatalf("NewReviewer: %v", err)
	}
	opts, err := ApprovalOptions(policy)
	if err != nil {
		t.Fatalf("ApprovalOptions: %v", err)
	}
	p, err := approval.New(append(opts, approval.WithReviewer(reviewer))...)
	if err != nil {
		t.Fatalf("approval.New: %v", err)
	}
	callbacks := plugin.MustNewManager(p).ToolCallbacks()

	result, err := callbacks.RunBeforeTool(context.Background(), &tool.BeforeToolArgs{ToolName: "workspace_write"})
	if err != nil {
		t.Fatalf("RunBeforeTool: %v", err)
	}
	if result != nil {
		t.Fatalf("expected approved ask flow to continue, got %+v", result)
	}
}

func newPolicy(t *testing.T, policy platform.ToolPolicy, opts ...Option) *Policy {
	t.Helper()
	policy = defaultPolicy(policy)
	p, err := New(policy, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func defaultPolicy(policy platform.ToolPolicy) platform.ToolPolicy {
	if strings.TrimSpace(policy.TenantID) == "" {
		policy.TenantID = "tenant"
	}
	if strings.TrimSpace(policy.AppID) == "" {
		policy.AppID = "app"
	}
	if strings.TrimSpace(policy.PolicyID) == "" {
		policy.PolicyID = "policy"
	}
	return policy
}

func request(name string, metadata tool.ToolMetadata, args ...[]byte) *tool.PermissionRequest {
	var payload []byte
	if len(args) > 0 {
		payload = args[0]
	}
	return &tool.PermissionRequest{
		ToolName:    name,
		Declaration: &tool.Declaration{Name: name},
		Arguments:   payload,
		Metadata:    metadata,
	}
}

type allowReviewer struct{}

func (allowReviewer) Review(context.Context, *review.Request) (*review.Decision, error) {
	return &review.Decision{Approved: true}, nil
}

type failingAuditSink struct{}

func (failingAuditSink) WriteAudit(context.Context, platform.AuditRecord) error {
	return errors.New("audit unavailable")
}

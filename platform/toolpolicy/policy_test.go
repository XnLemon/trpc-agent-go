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
	if !strings.HasPrefix(record.RedactedDetailRef, "sha256:") {
		t.Fatalf("expected digest summary, got %q", record.RedactedDetailRef)
	}
}

func TestPolicyNilRedactorStillDoesNotLeakArguments(t *testing.T) {
	audit := platform.NewInMemoryAuditSink()
	p := newPolicy(
		t,
		platform.ToolPolicy{
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
	_, err := New(platform.ToolPolicy{DangerousToolAction: platform.DangerousToolAction("bad")})
	if err == nil {
		t.Fatalf("expected invalid action to fail")
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
	reviewer, err := NewReviewer(platform.ToolPolicy{
		ToolWhitelist: []string{"search"},
	})
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
	reviewer, err := NewReviewer(platform.ToolPolicy{
		DangerousToolAction: platform.DangerousToolActionAsk,
		HighRiskTools:       []string{"workspace_write"},
	})
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
	policy := platform.ToolPolicy{
		ToolWhitelist:       []string{"workspace_write"},
		DangerousToolAction: platform.DangerousToolActionAsk,
		HighRiskTools:       []string{"workspace_write"},
	}
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
	p, err := New(policy, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
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

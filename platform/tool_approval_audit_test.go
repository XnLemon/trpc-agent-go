//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewToolApprovalAuditRecordBuildsRequestedRecord(t *testing.T) {
	createdAt := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	input := ToolApprovalAuditInput{
		TenantID:           " tenant ",
		AppID:              " app ",
		ToolName:           " workspace_write ",
		ToolCallID:         " call-1 ",
		Decision:           ToolApprovalDecisionRequested,
		DecisionReason:     "high-risk tool requires approval",
		RequestID:          " request-1 ",
		TraceID:            " trace-1 ",
		ArgumentSummaryRef: `args:sha256:0123456789abcdef args_bytes:128`,
		CreatedAt:          createdAt,
	}

	record, err := NewToolApprovalAuditRecord(input)
	if err != nil {
		t.Fatalf("new tool approval audit: %v", err)
	}
	if record.TenantID != "tenant" ||
		record.AppID != "app" ||
		record.ToolName != "workspace_write" ||
		record.Decision != "approval_requested" ||
		record.DecisionReason != "high-risk tool requires approval" ||
		record.RequestID != "request-1" ||
		record.TraceID != "trace-1" ||
		!record.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected record: %+v", record)
	}
	if record.UserIDHash != "" {
		t.Fatalf("requested approval should not require approver hash, got %+v", record)
	}
	if record.AuditID == "" || record.RedactionVersion != "platform-tool-approval-v1" {
		t.Fatalf("expected audit id and redaction version, got %+v", record)
	}
	if !strings.Contains(record.RedactedDetailRef, "tool_call_id:call-1") ||
		!strings.Contains(record.RedactedDetailRef, "args_ref_sha256:") ||
		!strings.Contains(record.RedactedDetailRef, "args_ref_bytes:") {
		t.Fatalf("unexpected redacted detail ref: %q", record.RedactedDetailRef)
	}
	if strings.Contains(record.RedactedDetailRef, "args:sha256:") ||
		strings.Contains(record.RedactedDetailRef, "args_bytes:128") {
		t.Fatalf("approval audit leaked raw argument summary: %q", record.RedactedDetailRef)
	}

	again, err := NewToolApprovalAuditRecord(input)
	if err != nil {
		t.Fatalf("new duplicate tool approval audit: %v", err)
	}
	if record.AuditID != again.AuditID {
		t.Fatalf("expected stable audit id, got %q and %q", record.AuditID, again.AuditID)
	}
}

func TestNewToolApprovalAuditRecordBuildsDecidedRecordWithApproverHash(t *testing.T) {
	record, err := NewToolApprovalAuditRecord(ToolApprovalAuditInput{
		TenantID:       "tenant",
		AppID:          "app",
		ToolName:       "workspace_write",
		ToolCallID:     "call-1",
		Decision:       ToolApprovalDecisionApproved,
		DecisionReason: "approved by security reviewer",
		ApproverUserID: "security@example.com",
		RequestID:      "request-1",
		TraceID:        "trace-1",
		CreatedAt:      time.Unix(200, 0),
	})
	if err != nil {
		t.Fatalf("new decided tool approval audit: %v", err)
	}
	if record.Decision != "approval_approved" ||
		record.UserIDHash == "" ||
		!strings.HasPrefix(record.UserIDHash, "user_hash_") ||
		!strings.Contains(record.RedactedDetailRef, "approver_hash:user_hash_") {
		t.Fatalf("expected decided approval with approver hash, got %+v", record)
	}
	if strings.Contains(record.UserIDHash, "security@example.com") ||
		strings.Contains(record.RedactedDetailRef, "security@example.com") {
		t.Fatalf("approval audit leaked raw approver id: %+v", record)
	}
}

func TestNewToolApprovalAuditRecordRejectsInvalidInputs(t *testing.T) {
	base := validToolApprovalAuditInput()

	missingTenant := base
	missingTenant.TenantID = " "
	if _, err := NewToolApprovalAuditRecord(missingTenant); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}

	missingTool := base
	missingTool.ToolName = " "
	if _, err := NewToolApprovalAuditRecord(missingTool); err == nil ||
		!strings.Contains(err.Error(), "tool_name") {
		t.Fatalf("expected tool name requirement, got %v", err)
	}

	missingCall := base
	missingCall.ToolCallID = " "
	if _, err := NewToolApprovalAuditRecord(missingCall); err == nil ||
		!strings.Contains(err.Error(), "tool_call_id") {
		t.Fatalf("expected tool call id requirement, got %v", err)
	}

	unknownDecision := base
	unknownDecision.Decision = "bypassed"
	if _, err := NewToolApprovalAuditRecord(unknownDecision); err == nil ||
		!strings.Contains(err.Error(), "invalid tool approval decision") {
		t.Fatalf("expected decision validation, got %v", err)
	}

	missingApprover := base
	missingApprover.Decision = ToolApprovalDecisionRejected
	if _, err := NewToolApprovalAuditRecord(missingApprover); err == nil ||
		!strings.Contains(err.Error(), "approver_user_id") {
		t.Fatalf("expected approver requirement, got %v", err)
	}
}

func TestNewToolApprovalAuditRecordRejectsSensitivePublicFields(t *testing.T) {
	input := validToolApprovalAuditInput()
	input.DecisionReason = "Authorization: Bearer raw-token"
	if _, err := NewToolApprovalAuditRecord(input); err == nil ||
		!strings.Contains(err.Error(), "decision_reason") {
		t.Fatalf("expected sensitive decision reason rejection, got %v", err)
	}

	input = validToolApprovalAuditInput()
	input.ArgumentSummaryRef = "token=sk-secret"
	if _, err := NewToolApprovalAuditRecord(input); err == nil ||
		!strings.Contains(err.Error(), "argument_summary_ref") {
		t.Fatalf("expected sensitive argument summary rejection, got %v", err)
	}
}

func validToolApprovalAuditInput() ToolApprovalAuditInput {
	return ToolApprovalAuditInput{
		TenantID:       "tenant",
		AppID:          "app",
		ToolName:       "workspace_write",
		ToolCallID:     "call-1",
		Decision:       ToolApprovalDecisionRequested,
		DecisionReason: "approval required",
		RequestID:      "request",
		TraceID:        "trace",
		CreatedAt:      time.Unix(100, 0),
	}
}

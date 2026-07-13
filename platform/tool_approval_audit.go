//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// ToolApprovalDecision is the externally visible tool approval audit decision.
type ToolApprovalDecision string

const (
	// ToolApprovalDecisionRequested records that a tool call requires approval.
	ToolApprovalDecisionRequested ToolApprovalDecision = "approval_requested"
	// ToolApprovalDecisionApproved records that a tool approval was granted.
	ToolApprovalDecisionApproved ToolApprovalDecision = "approval_approved"
	// ToolApprovalDecisionRejected records that a tool approval was rejected.
	ToolApprovalDecisionRejected ToolApprovalDecision = "approval_rejected"
)

// ToolApprovalAuditInput contains safe dimensions for a tool approval boundary.
type ToolApprovalAuditInput struct {
	TenantID           string
	AppID              string
	ToolName           string
	ToolCallID         string
	Decision           ToolApprovalDecision
	DecisionReason     string
	ApproverUserID     string
	RequestID          string
	TraceID            string
	ArgumentSummaryRef string
	CreatedAt          time.Time
}

// NewToolApprovalAuditRecord maps one tool approval boundary into a safe audit record.
func NewToolApprovalAuditRecord(input ToolApprovalAuditInput) (AuditRecord, error) {
	normalized, err := input.normalize()
	if err != nil {
		return AuditRecord{}, err
	}
	record := AuditRecord{
		TenantID:          normalized.TenantID,
		AppID:             normalized.AppID,
		AuditID:           normalized.auditID(),
		RequestID:         normalized.RequestID,
		TraceID:           normalized.TraceID,
		UserIDHash:        normalized.approverHash(),
		ToolName:          normalized.ToolName,
		Decision:          string(normalized.Decision),
		DecisionReason:    normalized.DecisionReason,
		RedactedDetailRef: normalized.detailRef(),
		RedactionVersion:  "platform-tool-approval-v1",
		CreatedAt:         normalized.CreatedAt,
	}
	if err := record.Validate(); err != nil {
		return AuditRecord{}, err
	}
	return record, nil
}

func (i ToolApprovalAuditInput) normalize() (ToolApprovalAuditInput, error) {
	i.TenantID = strings.TrimSpace(i.TenantID)
	if i.TenantID == "" {
		return ToolApprovalAuditInput{}, ErrTenantIDRequired
	}
	i.AppID = strings.TrimSpace(i.AppID)
	i.ToolName = strings.TrimSpace(i.ToolName)
	if i.ToolName == "" {
		return ToolApprovalAuditInput{}, fmt.Errorf("tool_name is required")
	}
	i.ToolCallID = strings.TrimSpace(i.ToolCallID)
	if i.ToolCallID == "" {
		return ToolApprovalAuditInput{}, fmt.Errorf("tool_call_id is required")
	}
	i.Decision = ToolApprovalDecision(strings.TrimSpace(string(i.Decision)))
	if !i.Decision.valid() {
		return ToolApprovalAuditInput{}, fmt.Errorf("invalid tool approval decision %q", i.Decision)
	}
	i.DecisionReason = strings.TrimSpace(i.DecisionReason)
	i.ApproverUserID = strings.TrimSpace(i.ApproverUserID)
	if i.Decision != ToolApprovalDecisionRequested && i.ApproverUserID == "" {
		return ToolApprovalAuditInput{}, fmt.Errorf("approver_user_id is required for decided approvals")
	}
	i.RequestID = strings.TrimSpace(i.RequestID)
	i.TraceID = strings.TrimSpace(i.TraceID)
	i.ArgumentSummaryRef = strings.TrimSpace(i.ArgumentSummaryRef)
	if err := validateAuditRedactedFields(
		safeTextField{"app_id", i.AppID},
		safeTextField{"tool_name", i.ToolName},
		safeTextField{"tool_call_id", i.ToolCallID},
		safeTextField{"decision", string(i.Decision)},
		safeTextField{"decision_reason", i.DecisionReason},
		safeTextField{"request_id", i.RequestID},
		safeTextField{"trace_id", i.TraceID},
		safeTextField{"argument_summary_ref", i.ArgumentSummaryRef},
	); err != nil {
		return ToolApprovalAuditInput{}, err
	}
	return i, nil
}

func (i ToolApprovalAuditInput) auditID() string {
	return AuditID(
		i.TenantID,
		i.AppID,
		i.ToolName,
		i.ToolCallID,
		string(i.Decision),
		i.ApproverUserID,
	)
}

func (i ToolApprovalAuditInput) approverHash() string {
	if i.ApproverUserID == "" {
		return ""
	}
	return UserIDHash(i.TenantID, "approval", i.ApproverUserID)
}

func (i ToolApprovalAuditInput) detailRef() string {
	parts := []string{
		"tool_call_id:" + i.ToolCallID,
	}
	if i.ArgumentSummaryRef != "" {
		sum := sha256.Sum256([]byte(i.ArgumentSummaryRef))
		parts = append(parts, "args_ref_sha256:"+hex.EncodeToString(sum[:]))
		parts = append(parts, fmt.Sprintf("args_ref_bytes:%d", len(i.ArgumentSummaryRef)))
	}
	if approverHash := i.approverHash(); approverHash != "" {
		parts = append(parts, "approver_hash:"+approverHash)
	}
	return strings.Join(parts, " ")
}

func (d ToolApprovalDecision) valid() bool {
	switch d {
	case ToolApprovalDecisionRequested,
		ToolApprovalDecisionApproved,
		ToolApprovalDecisionRejected:
		return true
	default:
		return false
	}
}

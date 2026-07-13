//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// OperationalAction names a high-risk operations or admin action.
type OperationalAction string

const (
	// OperationalActionDeleteTenant deletes or disables an entire tenant boundary.
	OperationalActionDeleteTenant OperationalAction = "delete_tenant"
	// OperationalActionSwitchStorageProfile changes the tenant/app storage route.
	OperationalActionSwitchStorageProfile OperationalAction = "switch_storage_profile"
	// OperationalActionDisableAudit disables or weakens audit capture.
	OperationalActionDisableAudit OperationalAction = "disable_audit"
	// OperationalActionExpandToolPermission expands tool access for an app.
	OperationalActionExpandToolPermission OperationalAction = "expand_tool_permission"
	// OperationalActionExecuteDataMigration runs an operational data migration.
	OperationalActionExecuteDataMigration OperationalAction = "execute_data_migration"
)

// OperationalActionDecision is the recorded outcome of an operations action boundary.
type OperationalActionDecision string

const (
	// OperationalActionDecisionApprovalRequired records a pending confirmation boundary.
	OperationalActionDecisionApprovalRequired OperationalActionDecision = "approval_required"
	// OperationalActionDecisionApproved records an approved operation.
	OperationalActionDecisionApproved OperationalActionDecision = "approved"
	// OperationalActionDecisionRejected records a rejected operation.
	OperationalActionDecisionRejected OperationalActionDecision = "rejected"
	// OperationalActionDecisionExecuted records a completed operation.
	OperationalActionDecisionExecuted OperationalActionDecision = "executed"
	// OperationalActionDecisionFailed records a failed operation attempt.
	OperationalActionDecisionFailed OperationalActionDecision = "failed"
)

// OperationalActionAuditInput contains safe dimensions for a high-risk operations audit record.
type OperationalActionAuditInput struct {
	TenantID            string
	AppID               string
	Action              OperationalAction
	OperationID         string
	ResourceType        string
	ResourceID          string
	ActorUserID         string
	ActorInternalUserID string
	ApproverUserID      string
	Decision            OperationalActionDecision
	DecisionReason      string
	RequestID           string
	TraceID             string
	DetailJSON          []byte
	CreatedAt           time.Time
}

// NewOperationalActionAuditRecord maps an operations action into a safe audit record.
func NewOperationalActionAuditRecord(input OperationalActionAuditInput) (AuditRecord, error) {
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
		InternalUserID:    normalized.ActorInternalUserID,
		UserIDHash:        normalized.actorHash(),
		ToolName:          "ops:" + string(normalized.Action),
		Decision:          string(normalized.Decision),
		DecisionReason:    normalized.DecisionReason,
		RedactedDetailRef: normalized.detailRef(),
		RedactionVersion:  "platform-operational-action-v1",
		CreatedAt:         normalized.CreatedAt,
	}
	if err := record.Validate(); err != nil {
		return AuditRecord{}, err
	}
	return record, nil
}

func (i OperationalActionAuditInput) normalize() (OperationalActionAuditInput, error) {
	i.TenantID = strings.TrimSpace(i.TenantID)
	if i.TenantID == "" {
		return OperationalActionAuditInput{}, ErrTenantIDRequired
	}
	i.AppID = strings.TrimSpace(i.AppID)
	i.Action = OperationalAction(strings.TrimSpace(string(i.Action)))
	if !i.Action.valid() {
		return OperationalActionAuditInput{}, fmt.Errorf("invalid operational action %q", i.Action)
	}
	i.OperationID = strings.TrimSpace(i.OperationID)
	if i.OperationID == "" {
		return OperationalActionAuditInput{}, fmt.Errorf("operation_id is required")
	}
	i.ResourceType = strings.TrimSpace(i.ResourceType)
	if i.ResourceType == "" {
		return OperationalActionAuditInput{}, fmt.Errorf("resource_type is required")
	}
	i.ResourceID = strings.TrimSpace(i.ResourceID)
	if i.ResourceID == "" {
		return OperationalActionAuditInput{}, fmt.Errorf("resource_id is required")
	}
	i.ActorUserID = strings.TrimSpace(i.ActorUserID)
	i.ActorInternalUserID = strings.TrimSpace(i.ActorInternalUserID)
	if i.ActorUserID == "" && i.ActorInternalUserID == "" {
		return OperationalActionAuditInput{}, fmt.Errorf("actor identity is required")
	}
	i.ApproverUserID = strings.TrimSpace(i.ApproverUserID)
	i.Decision = OperationalActionDecision(strings.TrimSpace(string(i.Decision)))
	if !i.Decision.valid() {
		return OperationalActionAuditInput{}, fmt.Errorf("invalid operational action decision %q", i.Decision)
	}
	i.DecisionReason = strings.TrimSpace(i.DecisionReason)
	i.RequestID = strings.TrimSpace(i.RequestID)
	i.TraceID = strings.TrimSpace(i.TraceID)
	i.DetailJSON = bytes.TrimSpace(i.DetailJSON)
	if len(i.DetailJSON) > 0 && !json.Valid(i.DetailJSON) {
		return OperationalActionAuditInput{}, fmt.Errorf("detail_json must be valid json")
	}
	if err := validateAuditRedactedFields(
		safeTextField{"app_id", i.AppID},
		safeTextField{"action", string(i.Action)},
		safeTextField{"operation_id", i.OperationID},
		safeTextField{"resource_type", i.ResourceType},
		safeTextField{"actor_internal_user_id", i.ActorInternalUserID},
		safeTextField{"decision", string(i.Decision)},
		safeTextField{"decision_reason", i.DecisionReason},
		safeTextField{"request_id", i.RequestID},
		safeTextField{"trace_id", i.TraceID},
	); err != nil {
		return OperationalActionAuditInput{}, err
	}
	return i, nil
}

func (i OperationalActionAuditInput) auditID() string {
	return AuditID(
		i.TenantID,
		i.AppID,
		string(i.Action),
		i.OperationID,
		i.ResourceType,
		i.ResourceID,
		string(i.Decision),
	)
}

func (i OperationalActionAuditInput) actorHash() string {
	actor := i.ActorUserID
	if actor == "" {
		actor = i.ActorInternalUserID
	}
	return UserIDHash(i.TenantID, "ops", actor)
}

func (i OperationalActionAuditInput) detailRef() string {
	parts := []string{
		"resource_type:" + i.ResourceType,
		"resource_hash:" + shortHash(i.TenantID, i.ResourceType, i.ResourceID),
	}
	if i.ApproverUserID != "" {
		parts = append(parts, "approver_hash:"+UserIDHash(i.TenantID, "ops", i.ApproverUserID))
	}
	if len(i.DetailJSON) > 0 {
		sum := sha256.Sum256(i.DetailJSON)
		parts = append(parts, fmt.Sprintf("detail_sha256:%s", hex.EncodeToString(sum[:])))
		parts = append(parts, fmt.Sprintf("detail_bytes:%d", len(i.DetailJSON)))
	}
	return strings.Join(parts, " ")
}

func (a OperationalAction) valid() bool {
	switch a {
	case OperationalActionDeleteTenant,
		OperationalActionSwitchStorageProfile,
		OperationalActionDisableAudit,
		OperationalActionExpandToolPermission,
		OperationalActionExecuteDataMigration:
		return true
	default:
		return false
	}
}

func (d OperationalActionDecision) valid() bool {
	switch d {
	case OperationalActionDecisionApprovalRequired,
		OperationalActionDecisionApproved,
		OperationalActionDecisionRejected,
		OperationalActionDecisionExecuted,
		OperationalActionDecisionFailed:
		return true
	default:
		return false
	}
}

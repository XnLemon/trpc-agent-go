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

func TestNewOperationalActionAuditRecordBuildsSafeRecord(t *testing.T) {
	createdAt := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	input := OperationalActionAuditInput{
		TenantID:            "tenant",
		AppID:               "app",
		Action:              OperationalActionDeleteTenant,
		OperationID:         "operation-1",
		ResourceType:        "tenant",
		ResourceID:          "tenant",
		ActorUserID:         "admin@example.com",
		ActorInternalUserID: "usr_admin",
		ApproverUserID:      "security@example.com",
		Decision:            OperationalActionDecisionApproved,
		DecisionReason:      "secondary confirmation accepted",
		RequestID:           "request-1",
		TraceID:             "trace-1",
		DetailJSON:          []byte(`{"password":"plain","target":"tenant"}`),
		CreatedAt:           createdAt,
	}

	record, err := NewOperationalActionAuditRecord(input)
	if err != nil {
		t.Fatalf("new operational action audit: %v", err)
	}
	if record.TenantID != "tenant" || record.AppID != "app" {
		t.Fatalf("unexpected owner: %+v", record)
	}
	if record.ToolName != "ops:delete_tenant" || record.Decision != "approved" {
		t.Fatalf("unexpected action decision: %+v", record)
	}
	if record.UserID != "" || record.UserIDHash == "" || !strings.HasPrefix(record.UserIDHash, "user_hash_") {
		t.Fatalf("expected hashed actor without raw user id, got %+v", record)
	}
	if record.InternalUserID != "usr_admin" {
		t.Fatalf("expected internal actor id, got %+v", record)
	}
	if !record.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected created_at to be retained, got %v", record.CreatedAt)
	}
	if record.AuditID == "" || record.RedactionVersion != "platform-operational-action-v1" {
		t.Fatalf("expected audit id and redaction version, got %+v", record)
	}
	if !strings.Contains(record.RedactedDetailRef, "resource_type:tenant") ||
		!strings.Contains(record.RedactedDetailRef, "resource_hash:") ||
		!strings.Contains(record.RedactedDetailRef, "approver_hash:user_hash_") ||
		!strings.Contains(record.RedactedDetailRef, "detail_sha256:") ||
		!strings.Contains(record.RedactedDetailRef, "detail_bytes:38") {
		t.Fatalf("unexpected redacted detail ref: %q", record.RedactedDetailRef)
	}
	if strings.Contains(record.RedactedDetailRef, "plain") ||
		strings.Contains(record.RedactedDetailRef, "password") ||
		strings.Contains(record.RedactedDetailRef, "tenant\"") ||
		strings.Contains(record.RedactedDetailRef, "admin@example.com") ||
		strings.Contains(record.RedactedDetailRef, "security@example.com") {
		t.Fatalf("audit detail leaked raw operation context: %q", record.RedactedDetailRef)
	}

	again, err := NewOperationalActionAuditRecord(input)
	if err != nil {
		t.Fatalf("new duplicate operational action audit: %v", err)
	}
	if record.AuditID != again.AuditID {
		t.Fatalf("expected stable audit id, got %q and %q", record.AuditID, again.AuditID)
	}

	nextOperation := input
	nextOperation.OperationID = "operation-2"
	nextRecord, err := NewOperationalActionAuditRecord(nextOperation)
	if err != nil {
		t.Fatalf("new next operational action audit: %v", err)
	}
	if record.AuditID == nextRecord.AuditID {
		t.Fatalf("expected operation id to scope audit boundary, got %q", record.AuditID)
	}
}

func TestNewOperationalActionAuditRecordAcceptsInternalActorOnly(t *testing.T) {
	input := validOperationalActionAuditInput()
	input.ActorUserID = ""
	input.ActorInternalUserID = "usr_internal"

	record, err := NewOperationalActionAuditRecord(input)
	if err != nil {
		t.Fatalf("new internal actor operational action audit: %v", err)
	}
	if record.InternalUserID != "usr_internal" {
		t.Fatalf("expected internal actor identity, got %+v", record)
	}
	if record.UserIDHash == "" || !strings.HasPrefix(record.UserIDHash, "user_hash_") {
		t.Fatalf("expected internal actor hash, got %+v", record)
	}
	if strings.Contains(record.UserIDHash, "usr_internal") {
		t.Fatalf("user hash leaked internal actor id: %q", record.UserIDHash)
	}
}

func TestNewOperationalActionAuditRecordRejectsInvalidInputs(t *testing.T) {
	base := validOperationalActionAuditInput()

	missingTenant := base
	missingTenant.TenantID = " "
	if _, err := NewOperationalActionAuditRecord(missingTenant); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}

	missingAction := base
	missingAction.Action = " "
	if _, err := NewOperationalActionAuditRecord(missingAction); err == nil ||
		!strings.Contains(err.Error(), "invalid operational action") {
		t.Fatalf("expected action requirement, got %v", err)
	}

	unknownAction := base
	unknownAction.Action = "drop_prod_database"
	if _, err := NewOperationalActionAuditRecord(unknownAction); err == nil ||
		!strings.Contains(err.Error(), "invalid operational action") {
		t.Fatalf("expected unknown action rejection, got %v", err)
	}

	missingOperation := base
	missingOperation.OperationID = " "
	if _, err := NewOperationalActionAuditRecord(missingOperation); err == nil ||
		!strings.Contains(err.Error(), "operation_id") {
		t.Fatalf("expected operation id requirement, got %v", err)
	}

	missingResource := base
	missingResource.ResourceID = " "
	if _, err := NewOperationalActionAuditRecord(missingResource); err == nil ||
		!strings.Contains(err.Error(), "resource_id") {
		t.Fatalf("expected resource id requirement, got %v", err)
	}

	missingActor := base
	missingActor.ActorUserID = " "
	missingActor.ActorInternalUserID = " "
	if _, err := NewOperationalActionAuditRecord(missingActor); err == nil ||
		!strings.Contains(err.Error(), "actor identity") {
		t.Fatalf("expected actor identity requirement, got %v", err)
	}

	missingDecision := base
	missingDecision.Decision = " "
	if _, err := NewOperationalActionAuditRecord(missingDecision); err == nil ||
		!strings.Contains(err.Error(), "invalid operational action decision") {
		t.Fatalf("expected decision requirement, got %v", err)
	}

	unknownDecision := base
	unknownDecision.Decision = "bypassed"
	if _, err := NewOperationalActionAuditRecord(unknownDecision); err == nil ||
		!strings.Contains(err.Error(), "invalid operational action decision") {
		t.Fatalf("expected unknown decision rejection, got %v", err)
	}

	invalidDetail := base
	invalidDetail.DetailJSON = []byte(`{"broken":`)
	if _, err := NewOperationalActionAuditRecord(invalidDetail); err == nil ||
		!strings.Contains(err.Error(), "detail_json") {
		t.Fatalf("expected detail json validation, got %v", err)
	}
}

func TestNewOperationalActionAuditRecordRejectsSensitivePublicFields(t *testing.T) {
	input := validOperationalActionAuditInput()
	input.DecisionReason = "Authorization: Bearer raw-token"
	if _, err := NewOperationalActionAuditRecord(input); err == nil ||
		!strings.Contains(err.Error(), "decision_reason") {
		t.Fatalf("expected sensitive decision reason rejection, got %v", err)
	}

	input = validOperationalActionAuditInput()
	input.Action = "sk-1234567890abcdef"
	if _, err := NewOperationalActionAuditRecord(input); err == nil ||
		!strings.Contains(err.Error(), "invalid operational action") {
		t.Fatalf("expected sensitive action rejection, got %v", err)
	}
}

func validOperationalActionAuditInput() OperationalActionAuditInput {
	return OperationalActionAuditInput{
		TenantID:     "tenant",
		AppID:        "app",
		Action:       OperationalActionSwitchStorageProfile,
		OperationID:  "operation",
		ResourceType: "storage_profile",
		ResourceID:   "profile-a",
		ActorUserID:  "admin",
		Decision:     OperationalActionDecisionApprovalRequired,
		RequestID:    "request",
		TraceID:      "trace",
		DetailJSON:   []byte(`{"profile_id":"profile-a"}`),
		CreatedAt:    time.Now(),
	}
}

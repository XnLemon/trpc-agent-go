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
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewSecretRotationStatusReportBuildsSafeReport(t *testing.T) {
	updatedAt := time.Date(2026, 7, 8, 13, 0, 0, 0, time.UTC)
	input := SecretRotationStatusInput{
		TenantID:      "tenant",
		AppID:         "app",
		ResourceType:  "model_profile",
		ResourceID:    "profile-a",
		SecretField:   "api_key_ref",
		PreviousRef:   "secret://model-key-v1",
		NextRef:       "kms://tenant/model-key-v2",
		Status:        SecretRotationStatusReady,
		OperationID:   "rotation-1",
		FailureReason: "verification passed",
		TraceID:       "trace-1",
		UpdatedAt:     updatedAt,
	}

	report, err := NewSecretRotationStatusReport(input)
	if err != nil {
		t.Fatalf("new secret rotation status report: %v", err)
	}
	if report.TenantID != "tenant" || report.AppID != "app" {
		t.Fatalf("unexpected owner: %+v", report)
	}
	if report.ResourceType != "model_profile" || report.ResourceHash == "" ||
		report.ResourceHash == "profile-a" {
		t.Fatalf("expected resource hash without raw resource id, got %+v", report)
	}
	if report.SecretField != "api_key_ref" ||
		report.PreviousRef != "secret://model-key-v1" ||
		report.NextRef != "kms://tenant/model-key-v2" ||
		report.Status != SecretRotationStatusReady {
		t.Fatalf("unexpected report fields: %+v", report)
	}
	if report.OperationID != "rotation-1" || report.TraceID != "trace-1" ||
		!report.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected operation metadata: %+v", report)
	}
	if !strings.HasPrefix(report.RotationID, "secret_rotation_") {
		t.Fatalf("unexpected rotation id: %q", report.RotationID)
	}
	serialized := fmt.Sprintf("%+v", report)
	if strings.Contains(serialized, "profile-a") ||
		strings.Contains(serialized, "plain-secret") {
		t.Fatalf("report leaked raw resource or secret content: %s", serialized)
	}

	again, err := NewSecretRotationStatusReport(input)
	if err != nil {
		t.Fatalf("new duplicate secret rotation status report: %v", err)
	}
	if report.RotationID != again.RotationID {
		t.Fatalf("expected stable rotation id, got %q and %q", report.RotationID, again.RotationID)
	}

	nextOperation := input
	nextOperation.OperationID = "rotation-2"
	nextReport, err := NewSecretRotationStatusReport(nextOperation)
	if err != nil {
		t.Fatalf("new next secret rotation status report: %v", err)
	}
	if report.RotationID == nextReport.RotationID {
		t.Fatalf("expected operation id to scope rotation id, got %q", report.RotationID)
	}
}

func TestNewSecretRotationStatusReportRejectsInvalidInputs(t *testing.T) {
	base := validSecretRotationStatusInput()

	missingTenant := base
	missingTenant.TenantID = " "
	if _, err := NewSecretRotationStatusReport(missingTenant); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}

	sensitiveTenant := base
	sensitiveTenant.TenantID = "tenant Authorization: Bearer raw-token"
	if _, err := NewSecretRotationStatusReport(sensitiveTenant); err == nil ||
		!strings.Contains(err.Error(), "tenant_id") {
		t.Fatalf("expected sensitive tenant rejection, got %v", err)
	}

	missingResource := base
	missingResource.ResourceID = " "
	if _, err := NewSecretRotationStatusReport(missingResource); err == nil ||
		!strings.Contains(err.Error(), "resource_id") {
		t.Fatalf("expected resource id requirement, got %v", err)
	}

	missingField := base
	missingField.SecretField = " "
	if _, err := NewSecretRotationStatusReport(missingField); err == nil ||
		!strings.Contains(err.Error(), "secret_field") {
		t.Fatalf("expected secret field requirement, got %v", err)
	}

	missingNextRef := base
	missingNextRef.NextRef = " "
	if _, err := NewSecretRotationStatusReport(missingNextRef); err == nil ||
		!strings.Contains(err.Error(), "next_ref") {
		t.Fatalf("expected next ref requirement, got %v", err)
	}

	inlineSecret := base
	inlineSecret.NextRef = "sk-1234567890abcdef"
	if _, err := NewSecretRotationStatusReport(inlineSecret); !errors.Is(err, ErrInlineSecretRejected) {
		t.Fatalf("expected inline secret rejection, got %v", err)
	}

	unknownStatus := base
	unknownStatus.Status = "skipped"
	if _, err := NewSecretRotationStatusReport(unknownStatus); err == nil ||
		!strings.Contains(err.Error(), "invalid secret rotation status") {
		t.Fatalf("expected status validation, got %v", err)
	}

	missingOperation := base
	missingOperation.OperationID = " "
	if _, err := NewSecretRotationStatusReport(missingOperation); err == nil ||
		!strings.Contains(err.Error(), "operation_id") {
		t.Fatalf("expected operation id requirement, got %v", err)
	}

	zeroUpdatedAt := base
	zeroUpdatedAt.UpdatedAt = time.Time{}
	if _, err := NewSecretRotationStatusReport(zeroUpdatedAt); err == nil ||
		!strings.Contains(err.Error(), "updated_at") {
		t.Fatalf("expected updated at requirement, got %v", err)
	}

	sensitiveFailure := base
	sensitiveFailure.FailureReason = "password=plain"
	if _, err := NewSecretRotationStatusReport(sensitiveFailure); err == nil ||
		!strings.Contains(err.Error(), "failure_reason") {
		t.Fatalf("expected sensitive failure reason rejection, got %v", err)
	}
}

func TestSecretRotationStatusReportValidateEnforcesStatusGates(t *testing.T) {
	base := validSecretRotationStatusInput()
	base.Status = SecretRotationStatusFailed
	base.FailureReason = ""
	if _, err := NewSecretRotationStatusReport(base); err == nil ||
		!strings.Contains(err.Error(), "failure_reason") {
		t.Fatalf("expected failed status to require failure reason, got %v", err)
	}

	base = validSecretRotationStatusInput()
	base.Status = SecretRotationStatusActive
	base.PreviousRef = ""
	if _, err := NewSecretRotationStatusReport(base); err == nil ||
		!strings.Contains(err.Error(), "previous_ref") {
		t.Fatalf("expected active status to require previous ref, got %v", err)
	}

	base = validSecretRotationStatusInput()
	base.Status = SecretRotationStatusRolledBack
	base.PreviousRef = ""
	if _, err := NewSecretRotationStatusReport(base); err == nil ||
		!strings.Contains(err.Error(), "previous_ref") {
		t.Fatalf("expected rolled back status to require previous ref, got %v", err)
	}
}

func TestSecretRotationStatusReportValidateRejectsUnsafeReport(t *testing.T) {
	generated, err := NewSecretRotationStatusReport(validSecretRotationStatusInput())
	if err != nil {
		t.Fatalf("new generated report: %v", err)
	}
	report := SecretRotationStatusReport{
		TenantID:      "tenant",
		AppID:         "app",
		RotationID:    generated.RotationID,
		ResourceType:  "channel_binding",
		ResourceHash:  generated.ResourceHash,
		SecretField:   "token_ref",
		PreviousRef:   "secret://token-v1",
		NextRef:       "secret://token-v2",
		Status:        SecretRotationStatusActive,
		OperationID:   "rotation",
		FailureReason: "cutover completed",
		TraceID:       "trace",
		UpdatedAt:     time.Now(),
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("expected report to validate: %v", err)
	}

	report.TenantID = "tenant Authorization: Bearer raw-token"
	if err := report.Validate(); err == nil ||
		!strings.Contains(err.Error(), "tenant_id") {
		t.Fatalf("expected sensitive tenant rejection, got %v", err)
	}

	report.TenantID = "tenant"
	report.NextRef = "postgres://user:password@example.com/db"
	if err := report.Validate(); !errors.Is(err, ErrInlineSecretRejected) {
		t.Fatalf("expected unsafe next ref rejection, got %v", err)
	}

	report.NextRef = "secret://token-v2"
	report.ResourceHash = "binding-a"
	if err := report.Validate(); err == nil ||
		!strings.Contains(err.Error(), "resource_hash") {
		t.Fatalf("expected raw resource hash rejection, got %v", err)
	}

	report.ResourceHash = generated.ResourceHash
	report.RotationID = "secret_rotation_binding-a"
	if err := report.Validate(); err == nil ||
		!strings.Contains(err.Error(), "rotation_id") {
		t.Fatalf("expected unsafe rotation id rejection, got %v", err)
	}
}

func TestNewSecretRotationStatusReportRequiresSafeReferenceFormat(t *testing.T) {
	base := validSecretRotationStatusInput()

	plaintext := base
	plaintext.NextRef = "ordinary-token-value"
	if _, err := NewSecretRotationStatusReport(plaintext); err == nil ||
		!strings.Contains(err.Error(), "next_ref") {
		t.Fatalf("expected plaintext next ref rejection, got %v", err)
	}

	unknownScheme := base
	unknownScheme.NextRef = "file://tenant/token"
	if _, err := NewSecretRotationStatusReport(unknownScheme); err == nil ||
		!strings.Contains(err.Error(), "next_ref") {
		t.Fatalf("expected unknown scheme rejection, got %v", err)
	}

	unsafePrevious := base
	unsafePrevious.PreviousRef = "plain-previous-token"
	if _, err := NewSecretRotationStatusReport(unsafePrevious); err == nil ||
		!strings.Contains(err.Error(), "previous_ref") {
		t.Fatalf("expected plaintext previous ref rejection, got %v", err)
	}

	for _, nextRef := range []string{
		"secret://token-v2",
		"kms://tenant/token-v2",
		"vault://secret/data/token-v2",
	} {
		input := base
		input.PreviousRef = ""
		input.NextRef = nextRef
		if _, err := NewSecretRotationStatusReport(input); err != nil {
			t.Fatalf("expected %q to validate: %v", nextRef, err)
		}
	}
}

func validSecretRotationStatusInput() SecretRotationStatusInput {
	return SecretRotationStatusInput{
		TenantID:     "tenant",
		AppID:        "app",
		ResourceType: "channel_binding",
		ResourceID:   "binding-a",
		SecretField:  "token_ref",
		PreviousRef:  "secret://token-v1",
		NextRef:      "secret://token-v2",
		Status:       SecretRotationStatusPending,
		OperationID:  "rotation",
		TraceID:      "trace",
		UpdatedAt:    time.Now(),
	}
}

//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"fmt"
	"strings"
	"time"
)

const secretRotationIDPrefix = "secret_rotation_"

// SecretRotationStatus describes the lifecycle state of one secret rotation.
type SecretRotationStatus string

const (
	// SecretRotationStatusPending means the new secret reference has been registered but not verified.
	SecretRotationStatusPending SecretRotationStatus = "pending"
	// SecretRotationStatusVerifying means dependent systems are validating the new secret reference.
	SecretRotationStatusVerifying SecretRotationStatus = "verifying"
	// SecretRotationStatusReady means the new secret reference is ready for cutover.
	SecretRotationStatusReady SecretRotationStatus = "ready"
	// SecretRotationStatusActive means traffic has moved to the new secret reference.
	SecretRotationStatusActive SecretRotationStatus = "active"
	// SecretRotationStatusRolledBack means traffic has returned to the previous secret reference.
	SecretRotationStatusRolledBack SecretRotationStatus = "rolled_back"
	// SecretRotationStatusFailed means the rotation failed before completion.
	SecretRotationStatusFailed SecretRotationStatus = "failed"
)

// SecretRotationStatusInput contains safe metadata for one secret rotation status update.
type SecretRotationStatusInput struct {
	TenantID      string
	AppID         string
	ResourceType  string
	ResourceID    string
	SecretField   string
	PreviousRef   string
	NextRef       string
	Status        SecretRotationStatus
	OperationID   string
	FailureReason string
	TraceID       string
	UpdatedAt     time.Time
}

// SecretRotationStatusReport is a safe, operations-facing secret rotation status.
type SecretRotationStatusReport struct {
	TenantID      string
	AppID         string
	RotationID    string
	ResourceType  string
	ResourceHash  string
	SecretField   string
	PreviousRef   string
	NextRef       string
	Status        SecretRotationStatus
	OperationID   string
	FailureReason string
	TraceID       string
	UpdatedAt     time.Time
}

// NewSecretRotationStatusReport builds a safe status report for secret rotation observability.
func NewSecretRotationStatusReport(input SecretRotationStatusInput) (SecretRotationStatusReport, error) {
	normalized, err := input.normalize()
	if err != nil {
		return SecretRotationStatusReport{}, err
	}
	resourceHash := shortHash(normalized.TenantID, normalized.ResourceType, normalized.ResourceID)
	report := SecretRotationStatusReport{
		TenantID:      normalized.TenantID,
		AppID:         normalized.AppID,
		RotationID:    normalized.rotationID(resourceHash),
		ResourceType:  normalized.ResourceType,
		ResourceHash:  resourceHash,
		SecretField:   normalized.SecretField,
		PreviousRef:   normalized.PreviousRef,
		NextRef:       normalized.NextRef,
		Status:        normalized.Status,
		OperationID:   normalized.OperationID,
		FailureReason: normalized.FailureReason,
		TraceID:       normalized.TraceID,
		UpdatedAt:     normalized.UpdatedAt,
	}
	if err := report.Validate(); err != nil {
		return SecretRotationStatusReport{}, err
	}
	return report, nil
}

// Validate checks that a secret rotation status report is safe to expose or store.
func (r SecretRotationStatusReport) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if err := validateAuditRedactedText("tenant_id", r.TenantID); err != nil {
		return err
	}
	if strings.TrimSpace(r.RotationID) == "" {
		return fmt.Errorf("rotation_id is required")
	}
	if !isSecretRotationID(r.RotationID) {
		return fmt.Errorf("rotation_id must be %s followed by a 24 character hex hash", secretRotationIDPrefix)
	}
	if strings.TrimSpace(r.ResourceType) == "" {
		return fmt.Errorf("resource_type is required")
	}
	if strings.TrimSpace(r.ResourceHash) == "" {
		return fmt.Errorf("resource_hash is required")
	}
	if !isShortHash(r.ResourceHash) {
		return fmt.Errorf("resource_hash must be a 24 character hex hash")
	}
	if strings.TrimSpace(r.SecretField) == "" {
		return fmt.Errorf("secret_field is required")
	}
	if err := validateRotationSecretReference("previous_ref", r.PreviousRef); err != nil {
		return err
	}
	if err := validateRotationSecretReference("next_ref", r.NextRef); err != nil {
		return err
	}
	if strings.TrimSpace(r.NextRef) == "" {
		return fmt.Errorf("next_ref is required")
	}
	if !r.Status.valid() {
		return fmt.Errorf("invalid secret rotation status %q", r.Status)
	}
	if strings.TrimSpace(r.OperationID) == "" {
		return fmt.Errorf("operation_id is required")
	}
	if expected := r.expectedRotationID(); r.RotationID != expected {
		return fmt.Errorf("rotation_id does not match report identity")
	}
	if r.UpdatedAt.IsZero() {
		return fmt.Errorf("updated_at is required")
	}
	if err := validateAuditRedactedFields(
		safeTextField{"app_id", r.AppID},
		safeTextField{"rotation_id", r.RotationID},
		safeTextField{"resource_type", r.ResourceType},
		safeTextField{"resource_hash", r.ResourceHash},
		safeTextField{"secret_field", r.SecretField},
		safeTextField{"operation_id", r.OperationID},
		safeTextField{"failure_reason", r.FailureReason},
		safeTextField{"trace_id", r.TraceID},
	); err != nil {
		return err
	}
	return validateSecretRotationStatusGate(r)
}

func (i SecretRotationStatusInput) normalize() (SecretRotationStatusInput, error) {
	i.TenantID = strings.TrimSpace(i.TenantID)
	if i.TenantID == "" {
		return SecretRotationStatusInput{}, ErrTenantIDRequired
	}
	if err := validateAuditRedactedText("tenant_id", i.TenantID); err != nil {
		return SecretRotationStatusInput{}, err
	}
	i.AppID = strings.TrimSpace(i.AppID)
	i.ResourceType = strings.TrimSpace(i.ResourceType)
	if i.ResourceType == "" {
		return SecretRotationStatusInput{}, fmt.Errorf("resource_type is required")
	}
	i.ResourceID = strings.TrimSpace(i.ResourceID)
	if i.ResourceID == "" {
		return SecretRotationStatusInput{}, fmt.Errorf("resource_id is required")
	}
	i.SecretField = strings.TrimSpace(i.SecretField)
	if i.SecretField == "" {
		return SecretRotationStatusInput{}, fmt.Errorf("secret_field is required")
	}
	i.PreviousRef = strings.TrimSpace(i.PreviousRef)
	i.NextRef = strings.TrimSpace(i.NextRef)
	if err := validateRotationSecretReference("previous_ref", i.PreviousRef); err != nil {
		return SecretRotationStatusInput{}, err
	}
	if err := validateRotationSecretReference("next_ref", i.NextRef); err != nil {
		return SecretRotationStatusInput{}, err
	}
	if i.NextRef == "" {
		return SecretRotationStatusInput{}, fmt.Errorf("next_ref is required")
	}
	i.Status = SecretRotationStatus(strings.TrimSpace(string(i.Status)))
	if !i.Status.valid() {
		return SecretRotationStatusInput{}, fmt.Errorf("invalid secret rotation status %q", i.Status)
	}
	i.OperationID = strings.TrimSpace(i.OperationID)
	if i.OperationID == "" {
		return SecretRotationStatusInput{}, fmt.Errorf("operation_id is required")
	}
	i.FailureReason = strings.TrimSpace(i.FailureReason)
	i.TraceID = strings.TrimSpace(i.TraceID)
	if i.UpdatedAt.IsZero() {
		return SecretRotationStatusInput{}, fmt.Errorf("updated_at is required")
	}
	if err := validateAuditRedactedFields(
		safeTextField{"app_id", i.AppID},
		safeTextField{"resource_type", i.ResourceType},
		safeTextField{"secret_field", i.SecretField},
		safeTextField{"operation_id", i.OperationID},
		safeTextField{"failure_reason", i.FailureReason},
		safeTextField{"trace_id", i.TraceID},
	); err != nil {
		return SecretRotationStatusInput{}, err
	}
	return i, nil
}

func validateSecretRotationStatusGate(r SecretRotationStatusReport) error {
	switch r.Status {
	case SecretRotationStatusFailed:
		if strings.TrimSpace(r.FailureReason) == "" {
			return fmt.Errorf("failure_reason is required when secret rotation status is failed")
		}
	case SecretRotationStatusActive, SecretRotationStatusRolledBack:
		if strings.TrimSpace(r.PreviousRef) == "" {
			return fmt.Errorf("previous_ref is required when secret rotation status is %s", r.Status)
		}
	}
	return nil
}

func (i SecretRotationStatusInput) rotationID(resourceHash string) string {
	return secretRotationIDPrefix + shortHash(
		i.TenantID,
		i.AppID,
		i.ResourceType,
		resourceHash,
		i.SecretField,
		i.OperationID,
	)
}

func (r SecretRotationStatusReport) expectedRotationID() string {
	return secretRotationIDPrefix + shortHash(
		r.TenantID,
		r.AppID,
		r.ResourceType,
		r.ResourceHash,
		r.SecretField,
		r.OperationID,
	)
}

func (s SecretRotationStatus) valid() bool {
	switch s {
	case SecretRotationStatusPending,
		SecretRotationStatusVerifying,
		SecretRotationStatusReady,
		SecretRotationStatusActive,
		SecretRotationStatusRolledBack,
		SecretRotationStatusFailed:
		return true
	default:
		return false
	}
}

func validateRotationSecretReference(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if err := validateSecretReference(field, value); err != nil {
		return err
	}
	if !isAllowedSecretReference(value) {
		return fmt.Errorf("%s must use secret://, kms://, or vault:// reference format", field)
	}
	return nil
}

func isAllowedSecretReference(value string) bool {
	switch {
	case strings.HasPrefix(value, "secret://"):
		return len(strings.TrimPrefix(value, "secret://")) > 0
	case strings.HasPrefix(value, "kms://"):
		return len(strings.TrimPrefix(value, "kms://")) > 0
	case strings.HasPrefix(value, "vault://"):
		return len(strings.TrimPrefix(value, "vault://")) > 0
	default:
		return false
	}
}

func isSecretRotationID(value string) bool {
	if !strings.HasPrefix(value, secretRotationIDPrefix) {
		return false
	}
	return isShortHash(strings.TrimPrefix(value, secretRotationIDPrefix))
}

func isShortHash(value string) bool {
	if len(value) != 24 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

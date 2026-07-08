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

// AppConfigCacheInvalidationReason explains why active config cache must be invalidated.
type AppConfigCacheInvalidationReason string

const (
	// AppConfigCacheInvalidationReasonActivate follows a normal release activation.
	AppConfigCacheInvalidationReasonActivate AppConfigCacheInvalidationReason = "activate"
	// AppConfigCacheInvalidationReasonRollback follows an operational rollback.
	AppConfigCacheInvalidationReasonRollback AppConfigCacheInvalidationReason = "rollback"
)

// AppConfigCacheInvalidationInput describes one active config version switch.
type AppConfigCacheInvalidationInput struct {
	PreviousVersion AppConfigVersion
	NextVersion     AppConfigVersion
	Reason          AppConfigCacheInvalidationReason
	OperationID     string
	TraceID         string
	CreatedAt       time.Time
}

// AppConfigCacheInvalidation is a safe marker for invalidating active config caches.
type AppConfigCacheInvalidation struct {
	TenantID         string
	AppID            string
	InvalidationID   string
	CacheKey         string
	PreviousVersion  string
	PreviousChecksum string
	NextVersion      string
	NextChecksum     string
	Reason           AppConfigCacheInvalidationReason
	OperationID      string
	TraceID          string
	CreatedAt        time.Time
}

// NewAppConfigCacheInvalidation builds a cache invalidation marker for an active config switch.
func NewAppConfigCacheInvalidation(input AppConfigCacheInvalidationInput) (AppConfigCacheInvalidation, error) {
	normalized, err := input.normalize()
	if err != nil {
		return AppConfigCacheInvalidation{}, err
	}
	marker := AppConfigCacheInvalidation{
		TenantID:         strings.TrimSpace(normalized.NextVersion.TenantID),
		AppID:            strings.TrimSpace(normalized.NextVersion.AppID),
		InvalidationID:   normalized.invalidationID(),
		CacheKey:         activeConfigCacheKey(normalized.NextVersion.TenantID, normalized.NextVersion.AppID),
		PreviousVersion:  strings.TrimSpace(normalized.PreviousVersion.Version),
		PreviousChecksum: strings.TrimSpace(normalized.PreviousVersion.Checksum),
		NextVersion:      strings.TrimSpace(normalized.NextVersion.Version),
		NextChecksum:     strings.TrimSpace(normalized.NextVersion.Checksum),
		Reason:           normalized.Reason,
		OperationID:      normalized.OperationID,
		TraceID:          normalized.TraceID,
		CreatedAt:        normalized.CreatedAt,
	}
	if err := marker.Validate(); err != nil {
		return AppConfigCacheInvalidation{}, err
	}
	return marker, nil
}

// Validate checks that a cache invalidation marker is safe to emit or store.
func (m AppConfigCacheInvalidation) Validate() error {
	if strings.TrimSpace(m.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(m.AppID) == "" {
		return ErrAppIDRequired
	}
	if strings.TrimSpace(m.InvalidationID) == "" {
		return fmt.Errorf("invalidation_id is required")
	}
	if strings.TrimSpace(m.CacheKey) == "" {
		return fmt.Errorf("cache_key is required")
	}
	if strings.TrimSpace(m.PreviousVersion) == "" {
		return fmt.Errorf("previous_version is required")
	}
	if strings.TrimSpace(m.NextVersion) == "" {
		return fmt.Errorf("next_version is required")
	}
	if strings.TrimSpace(m.PreviousChecksum) == "" || strings.TrimSpace(m.NextChecksum) == "" {
		return fmt.Errorf("config checksums are required")
	}
	if !m.Reason.valid() {
		return fmt.Errorf("invalid config cache invalidation reason %q", m.Reason)
	}
	if strings.TrimSpace(m.OperationID) == "" {
		return fmt.Errorf("operation_id is required")
	}
	if m.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	for field, value := range map[string]string{
		"invalidation_id":   m.InvalidationID,
		"cache_key":         m.CacheKey,
		"previous_version":  m.PreviousVersion,
		"previous_checksum": m.PreviousChecksum,
		"next_version":      m.NextVersion,
		"next_checksum":     m.NextChecksum,
		"operation_id":      m.OperationID,
		"trace_id":          m.TraceID,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return err
		}
	}
	return nil
}

func (i AppConfigCacheInvalidationInput) normalize() (AppConfigCacheInvalidationInput, error) {
	if err := i.PreviousVersion.Validate(); err != nil {
		return AppConfigCacheInvalidationInput{}, fmt.Errorf("previous config version: %w", err)
	}
	if err := i.NextVersion.Validate(); err != nil {
		return AppConfigCacheInvalidationInput{}, fmt.Errorf("next config version: %w", err)
	}
	if err := requireSameConfigOwner(i.PreviousVersion, i.NextVersion); err != nil {
		return AppConfigCacheInvalidationInput{}, err
	}
	if i.NextVersion.Status != AppConfigVersionStatusActive {
		return AppConfigCacheInvalidationInput{}, fmt.Errorf("next config version status must be active")
	}
	if strings.TrimSpace(i.PreviousVersion.Version) == strings.TrimSpace(i.NextVersion.Version) {
		return AppConfigCacheInvalidationInput{}, fmt.Errorf("config version switch must change version")
	}
	i.Reason = AppConfigCacheInvalidationReason(strings.TrimSpace(string(i.Reason)))
	if !i.Reason.valid() {
		return AppConfigCacheInvalidationInput{}, fmt.Errorf("invalid config cache invalidation reason %q", i.Reason)
	}
	i.OperationID = strings.TrimSpace(i.OperationID)
	if i.OperationID == "" {
		return AppConfigCacheInvalidationInput{}, fmt.Errorf("operation_id is required")
	}
	i.TraceID = strings.TrimSpace(i.TraceID)
	if i.CreatedAt.IsZero() {
		return AppConfigCacheInvalidationInput{}, fmt.Errorf("created_at is required")
	}
	for field, value := range map[string]string{
		"operation_id": i.OperationID,
		"trace_id":     i.TraceID,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return AppConfigCacheInvalidationInput{}, err
		}
	}
	return i, nil
}

func (i AppConfigCacheInvalidationInput) invalidationID() string {
	return "config_invalidation_" + shortHash(
		strings.TrimSpace(i.PreviousVersion.TenantID),
		strings.TrimSpace(i.PreviousVersion.AppID),
		strings.TrimSpace(i.PreviousVersion.Version),
		strings.TrimSpace(i.NextVersion.Version),
		string(i.Reason),
		i.OperationID,
	)
}

func activeConfigCacheKey(tenantID, appID string) string {
	return strings.Join([]string{
		"tenant", escapeKeyPart(tenantID),
		"app", escapeKeyPart(appID),
		"config", "active",
	}, ":")
}

func (r AppConfigCacheInvalidationReason) valid() bool {
	switch r {
	case AppConfigCacheInvalidationReasonActivate,
		AppConfigCacheInvalidationReasonRollback:
		return true
	default:
		return false
	}
}

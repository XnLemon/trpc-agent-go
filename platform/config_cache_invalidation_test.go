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

func TestNewAppConfigCacheInvalidationBuildsRollbackMarker(t *testing.T) {
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	previous := validLifecycleConfigVersion("v2", AppConfigVersionStatusRollback)
	previous.Checksum = "sha256:previous"
	next := validLifecycleConfigVersion("v1", AppConfigVersionStatusActive)
	next.Checksum = "sha256:next"

	marker, err := NewAppConfigCacheInvalidation(AppConfigCacheInvalidationInput{
		PreviousVersion: previous,
		NextVersion:     next,
		Reason:          AppConfigCacheInvalidationReasonRollback,
		OperationID:     "rollback-1",
		TraceID:         "trace-1",
		CreatedAt:       createdAt,
	})
	if err != nil {
		t.Fatalf("new config cache invalidation: %v", err)
	}
	if marker.TenantID != "tenant" || marker.AppID != "app" {
		t.Fatalf("unexpected owner: %+v", marker)
	}
	if marker.CacheKey != "tenant:tenant:app:app:config:active" {
		t.Fatalf("unexpected cache key: %q", marker.CacheKey)
	}
	if marker.PreviousVersion != "v2" || marker.PreviousChecksum != "sha256:previous" ||
		marker.NextVersion != "v1" || marker.NextChecksum != "sha256:next" {
		t.Fatalf("unexpected version switch summary: %+v", marker)
	}
	if marker.Reason != AppConfigCacheInvalidationReasonRollback ||
		marker.OperationID != "rollback-1" || marker.TraceID != "trace-1" ||
		!marker.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected marker metadata: %+v", marker)
	}
	if !strings.HasPrefix(marker.InvalidationID, "config_invalidation_") {
		t.Fatalf("unexpected invalidation id: %q", marker.InvalidationID)
	}
	serialized := fmt.Sprintf("%+v", marker)
	if strings.Contains(serialized, "model_profile_id") ||
		strings.Contains(serialized, "tool_policy_id") ||
		strings.Contains(serialized, "api_key_ref") {
		t.Fatalf("marker leaked config bundle content: %s", serialized)
	}

	again, err := NewAppConfigCacheInvalidation(AppConfigCacheInvalidationInput{
		PreviousVersion: previous,
		NextVersion:     next,
		Reason:          AppConfigCacheInvalidationReasonRollback,
		OperationID:     "rollback-1",
		TraceID:         "trace-2",
		CreatedAt:       createdAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("new duplicate config cache invalidation: %v", err)
	}
	if marker.InvalidationID != again.InvalidationID {
		t.Fatalf("expected stable invalidation id, got %q and %q", marker.InvalidationID, again.InvalidationID)
	}

	nextOperation, err := NewAppConfigCacheInvalidation(AppConfigCacheInvalidationInput{
		PreviousVersion: previous,
		NextVersion:     next,
		Reason:          AppConfigCacheInvalidationReasonRollback,
		OperationID:     "rollback-2",
		CreatedAt:       createdAt,
	})
	if err != nil {
		t.Fatalf("new next config cache invalidation: %v", err)
	}
	if marker.InvalidationID == nextOperation.InvalidationID {
		t.Fatalf("expected operation id to scope invalidation id, got %q", marker.InvalidationID)
	}

	whitespace := previous
	whitespace.TenantID = " tenant "
	whitespace.AppID = " app "
	whitespace.Version = " v2 "
	whitespace.Checksum = " sha256:previous "
	whitespaceNext := next
	whitespaceNext.TenantID = " tenant "
	whitespaceNext.AppID = " app "
	whitespaceNext.Version = " v1 "
	whitespaceNext.Checksum = " sha256:next "
	trimmed, err := NewAppConfigCacheInvalidation(AppConfigCacheInvalidationInput{
		PreviousVersion: whitespace,
		NextVersion:     whitespaceNext,
		Reason:          AppConfigCacheInvalidationReasonRollback,
		OperationID:     "rollback-1",
		CreatedAt:       createdAt,
	})
	if err != nil {
		t.Fatalf("new trimmed config cache invalidation: %v", err)
	}
	if marker.InvalidationID != trimmed.InvalidationID {
		t.Fatalf("expected trimmed identity to keep stable invalidation id, got %q and %q", marker.InvalidationID, trimmed.InvalidationID)
	}
	if trimmed.PreviousVersion != "v2" || trimmed.NextVersion != "v1" ||
		trimmed.PreviousChecksum != "sha256:previous" || trimmed.NextChecksum != "sha256:next" {
		t.Fatalf("expected marker fields to be trimmed, got %+v", trimmed)
	}
}

func TestNewAppConfigCacheInvalidationBuildsActivationMarker(t *testing.T) {
	previous := validLifecycleConfigVersion("v1", AppConfigVersionStatusRollback)
	next := validLifecycleConfigVersion("v2", AppConfigVersionStatusActive)

	marker, err := NewAppConfigCacheInvalidation(AppConfigCacheInvalidationInput{
		PreviousVersion: previous,
		NextVersion:     next,
		Reason:          AppConfigCacheInvalidationReasonActivate,
		OperationID:     "activate-1",
		CreatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("new activation invalidation: %v", err)
	}
	if marker.Reason != AppConfigCacheInvalidationReasonActivate ||
		marker.PreviousVersion != "v1" || marker.NextVersion != "v2" {
		t.Fatalf("unexpected activation marker: %+v", marker)
	}
}

func TestNewAppConfigCacheInvalidationRejectsInvalidInputs(t *testing.T) {
	base := validAppConfigCacheInvalidationInput()

	missingTenant := base
	missingTenant.NextVersion.TenantID = " "
	if _, err := NewAppConfigCacheInvalidation(missingTenant); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}

	mismatch := base
	mismatch.NextVersion.AppID = "other-app"
	if _, err := NewAppConfigCacheInvalidation(mismatch); err == nil ||
		!strings.Contains(err.Error(), "app_id") {
		t.Fatalf("expected app mismatch error, got %v", err)
	}

	nextNotActive := base
	nextNotActive.NextVersion.Status = AppConfigVersionStatusReleased
	if _, err := NewAppConfigCacheInvalidation(nextNotActive); err == nil ||
		!strings.Contains(err.Error(), "active") {
		t.Fatalf("expected next active status error, got %v", err)
	}

	sameVersion := base
	sameVersion.NextVersion.Version = sameVersion.PreviousVersion.Version
	if _, err := NewAppConfigCacheInvalidation(sameVersion); err == nil ||
		!strings.Contains(err.Error(), "change version") {
		t.Fatalf("expected version switch validation, got %v", err)
	}

	missingReason := base
	missingReason.Reason = " "
	if _, err := NewAppConfigCacheInvalidation(missingReason); err == nil ||
		!strings.Contains(err.Error(), "invalid config cache invalidation reason") {
		t.Fatalf("expected reason validation, got %v", err)
	}

	missingOperation := base
	missingOperation.OperationID = " "
	if _, err := NewAppConfigCacheInvalidation(missingOperation); err == nil ||
		!strings.Contains(err.Error(), "operation_id") {
		t.Fatalf("expected operation id requirement, got %v", err)
	}

	zeroCreatedAt := base
	zeroCreatedAt.CreatedAt = time.Time{}
	if _, err := NewAppConfigCacheInvalidation(zeroCreatedAt); err == nil ||
		!strings.Contains(err.Error(), "created_at") {
		t.Fatalf("expected created_at requirement, got %v", err)
	}

	sensitiveTrace := base
	sensitiveTrace.TraceID = "Authorization: Bearer raw-token"
	if _, err := NewAppConfigCacheInvalidation(sensitiveTrace); err == nil ||
		!strings.Contains(err.Error(), "trace_id") {
		t.Fatalf("expected sensitive trace id rejection, got %v", err)
	}
}

func TestAppConfigCacheInvalidationValidateRejectsUnsafeMarker(t *testing.T) {
	marker := AppConfigCacheInvalidation{
		TenantID:         "tenant",
		AppID:            "app",
		InvalidationID:   "config_invalidation_id",
		CacheKey:         "tenant:tenant:app:app:config:active",
		PreviousVersion:  "v1",
		PreviousChecksum: "sha256:previous",
		NextVersion:      "v2",
		NextChecksum:     "sha256:next",
		Reason:           AppConfigCacheInvalidationReasonActivate,
		OperationID:      "operation",
		TraceID:          "trace",
		CreatedAt:        time.Now(),
	}
	if err := marker.Validate(); err != nil {
		t.Fatalf("expected marker to validate: %v", err)
	}

	marker.OperationID = "sk-1234567890abcdef"
	if err := marker.Validate(); err == nil || !strings.Contains(err.Error(), "operation_id") {
		t.Fatalf("expected operation id sensitive content rejection, got %v", err)
	}
}

func validAppConfigCacheInvalidationInput() AppConfigCacheInvalidationInput {
	return AppConfigCacheInvalidationInput{
		PreviousVersion: validLifecycleConfigVersion("v1", AppConfigVersionStatusRollback),
		NextVersion:     validLifecycleConfigVersion("v2", AppConfigVersionStatusActive),
		Reason:          AppConfigCacheInvalidationReasonActivate,
		OperationID:     "operation",
		TraceID:         "trace",
		CreatedAt:       time.Now(),
	}
}

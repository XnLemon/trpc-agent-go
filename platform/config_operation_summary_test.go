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

func TestNewAppConfigOperationSummaryBuildsActivationSummary(t *testing.T) {
	createdAt := time.Date(2026, 7, 8, 15, 0, 0, 0, time.UTC)
	previous := validLifecycleConfigVersion("v1", AppConfigVersionStatusRollback)
	previous.Checksum = "sha256:previous"
	next := validLifecycleConfigVersion("v2", AppConfigVersionStatusActive)
	next.Checksum = "sha256:next"
	candidate := validLifecycleConfigVersion("v3", AppConfigVersionStatusReleased)
	candidate.Checksum = "sha256:candidate"
	candidate.GrayPercent = 20

	summary, err := NewAppConfigOperationSummary(AppConfigOperationSummaryInput{
		Operation:      AppConfigOperationActivate,
		PreviousActive: previous,
		NextActive:     next,
		ResultVersions: []AppConfigVersion{previous, next, candidate},
		OperationID:    "activate-1",
		TraceID:        "trace-1",
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("new activation operation summary: %v", err)
	}
	if summary.TenantID != "tenant" || summary.AppID != "app" {
		t.Fatalf("unexpected owner: %+v", summary)
	}
	if summary.Operation != AppConfigOperationActivate ||
		summary.OperationID != "activate-1" ||
		!summary.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected operation metadata: %+v", summary)
	}
	if summary.PreviousVersion != "v1" || summary.PreviousChecksum != "sha256:previous" ||
		summary.NextVersion != "v2" || summary.NextChecksum != "sha256:next" {
		t.Fatalf("unexpected version summary: %+v", summary)
	}
	if !strings.HasPrefix(summary.SummaryID, configOperationSummaryIDPrefix) {
		t.Fatalf("unexpected summary id: %q", summary.SummaryID)
	}
	if summary.DiffChangeCount <= 0 || !summary.RequiresCacheFlush {
		t.Fatalf("expected positive diff and cache flush: %+v", summary)
	}
	if summary.CacheInvalidation.Reason != AppConfigCacheInvalidationReasonActivate ||
		summary.CacheInvalidation.OperationID != "activate-1" ||
		summary.CacheInvalidation.NextVersion != "v2" {
		t.Fatalf("unexpected cache invalidation marker: %+v", summary.CacheInvalidation)
	}
	if summary.GrayStatus.ActiveVersion != "v2" ||
		!summary.GrayStatus.HasCandidate ||
		summary.GrayStatus.CandidateVersion != "v3" ||
		summary.GrayStatus.CandidateTrafficPercent != 20 {
		t.Fatalf("unexpected gray status: %+v", summary.GrayStatus)
	}
	serialized := fmt.Sprintf("%+v", summary)
	if strings.Contains(serialized, "model_profile_id") ||
		strings.Contains(serialized, "tool_policy_id") ||
		strings.Contains(serialized, "api_key_ref") {
		t.Fatalf("summary leaked config bundle content: %s", serialized)
	}

	again, err := NewAppConfigOperationSummary(AppConfigOperationSummaryInput{
		Operation:      AppConfigOperationActivate,
		PreviousActive: previous,
		NextActive:     next,
		ResultVersions: []AppConfigVersion{previous, next, candidate},
		OperationID:    "activate-1",
		TraceID:        "trace-2",
		CreatedAt:      createdAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("new duplicate operation summary: %v", err)
	}
	if summary.SummaryID != again.SummaryID {
		t.Fatalf("expected stable summary id, got %q and %q", summary.SummaryID, again.SummaryID)
	}

	nextOperation, err := NewAppConfigOperationSummary(AppConfigOperationSummaryInput{
		Operation:      AppConfigOperationActivate,
		PreviousActive: previous,
		NextActive:     next,
		ResultVersions: []AppConfigVersion{previous, next, candidate},
		OperationID:    "activate-2",
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("new next operation summary: %v", err)
	}
	if summary.SummaryID == nextOperation.SummaryID {
		t.Fatalf("expected operation id to scope summary id, got %q", summary.SummaryID)
	}
}

func TestNewAppConfigOperationSummaryBuildsRollbackSummary(t *testing.T) {
	previous := validLifecycleConfigVersion("v2", AppConfigVersionStatusRollback)
	previous.Checksum = "sha256:previous-active"
	next := validLifecycleConfigVersion("v1", AppConfigVersionStatusActive)
	next.Checksum = "sha256:rollback-active"

	summary, err := NewAppConfigOperationSummary(AppConfigOperationSummaryInput{
		Operation:      AppConfigOperationRollback,
		PreviousActive: previous,
		NextActive:     next,
		ResultVersions: []AppConfigVersion{previous, next},
		OperationID:    "rollback-1",
		CreatedAt:      time.Now(),
	})
	if err != nil {
		t.Fatalf("new rollback operation summary: %v", err)
	}
	if summary.Operation != AppConfigOperationRollback ||
		summary.CacheInvalidation.Reason != AppConfigCacheInvalidationReasonRollback ||
		summary.GrayStatus.ActiveVersion != "v1" {
		t.Fatalf("unexpected rollback summary: %+v", summary)
	}
}

func TestNewAppConfigOperationSummaryRejectsInvalidInputs(t *testing.T) {
	base := validAppConfigOperationSummaryInput()

	unknownOperation := base
	unknownOperation.Operation = "promote"
	if _, err := NewAppConfigOperationSummary(unknownOperation); err == nil ||
		!strings.Contains(err.Error(), "invalid config operation") {
		t.Fatalf("expected operation validation, got %v", err)
	}

	missingTenant := base
	missingTenant.NextActive.TenantID = " "
	if _, err := NewAppConfigOperationSummary(missingTenant); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}

	previousNotRollback := base
	previousNotRollback.PreviousActive.Status = AppConfigVersionStatusActive
	if _, err := NewAppConfigOperationSummary(previousNotRollback); err == nil ||
		!strings.Contains(err.Error(), "rollback") {
		t.Fatalf("expected previous rollback status requirement, got %v", err)
	}

	nextNotActive := base
	nextNotActive.NextActive.Status = AppConfigVersionStatusReleased
	if _, err := NewAppConfigOperationSummary(nextNotActive); err == nil ||
		!strings.Contains(err.Error(), "active") {
		t.Fatalf("expected next active status requirement, got %v", err)
	}

	sameVersion := base
	sameVersion.NextActive.Version = sameVersion.PreviousActive.Version
	if _, err := NewAppConfigOperationSummary(sameVersion); err == nil ||
		!strings.Contains(err.Error(), "change active version") {
		t.Fatalf("expected version switch validation, got %v", err)
	}

	missingOperation := base
	missingOperation.OperationID = " "
	if _, err := NewAppConfigOperationSummary(missingOperation); err == nil ||
		!strings.Contains(err.Error(), "operation_id") {
		t.Fatalf("expected operation id requirement, got %v", err)
	}

	missingResults := base
	missingResults.ResultVersions = nil
	if _, err := NewAppConfigOperationSummary(missingResults); err == nil ||
		!strings.Contains(err.Error(), "result_versions") {
		t.Fatalf("expected result versions requirement, got %v", err)
	}

	mismatchedResult := base
	mismatchedResult.ResultVersions = append([]AppConfigVersion(nil), base.ResultVersions...)
	mismatchedResult.ResultVersions[0].AppID = "other-app"
	if _, err := NewAppConfigOperationSummary(mismatchedResult); err == nil ||
		!strings.Contains(err.Error(), "app_id") {
		t.Fatalf("expected result version app mismatch, got %v", err)
	}

	zeroCreatedAt := base
	zeroCreatedAt.CreatedAt = time.Time{}
	if _, err := NewAppConfigOperationSummary(zeroCreatedAt); err == nil ||
		!strings.Contains(err.Error(), "created_at") {
		t.Fatalf("expected created at requirement, got %v", err)
	}

	sensitiveTrace := validAppConfigOperationSummaryInput()
	sensitiveTrace.TraceID = "Authorization: Bearer raw-token"
	if _, err := NewAppConfigOperationSummary(sensitiveTrace); err == nil ||
		!strings.Contains(err.Error(), "trace_id") {
		t.Fatalf("expected sensitive trace rejection, got %v", err)
	}
}

func TestAppConfigOperationSummaryValidateRejectsUnsafeSummary(t *testing.T) {
	generated, err := NewAppConfigOperationSummary(validAppConfigOperationSummaryInput())
	if err != nil {
		t.Fatalf("new generated config operation summary: %v", err)
	}
	summary := generated
	if err := summary.Validate(); err != nil {
		t.Fatalf("expected summary to validate: %v", err)
	}

	summary.SummaryID = "config_operation_v1"
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "summary_id") {
		t.Fatalf("expected unsafe summary id rejection, got %v", err)
	}

	summary = generated
	summary.OperationID = "activate-2"
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "summary_id") {
		t.Fatalf("expected stale summary id rejection, got %v", err)
	}

	summary = generated
	summary.RequiresCacheFlush = false
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "requires_cache_flush") {
		t.Fatalf("expected cache flush requirement, got %v", err)
	}

	summary = generated
	summary.DiffChangeCount = 0
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "diff_change_count") {
		t.Fatalf("expected positive diff count requirement, got %v", err)
	}

	summary = generated
	summary.GrayStatus.ActiveVersion = "other-version"
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "gray_status") {
		t.Fatalf("expected gray status consistency rejection, got %v", err)
	}

	summary = generated
	summary.CacheInvalidation.Reason = AppConfigCacheInvalidationReasonRollback
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "cache_invalidation") {
		t.Fatalf("expected cache invalidation reason mismatch rejection, got %v", err)
	}

	summary = generated
	summary.CacheInvalidation.OperationID = "activate-2"
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "cache_invalidation") {
		t.Fatalf("expected cache invalidation operation mismatch rejection, got %v", err)
	}

	summary = generated
	summary.CacheInvalidation.NextVersion = "v3"
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "cache_invalidation") {
		t.Fatalf("expected cache invalidation version mismatch rejection, got %v", err)
	}

	summary = generated
	summary.CacheInvalidation.CreatedAt = summary.CreatedAt.Add(time.Minute)
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "cache_invalidation") {
		t.Fatalf("expected cache invalidation time mismatch rejection, got %v", err)
	}

	summary = generated
	summary.GrayStatus.HasCandidate = true
	summary.GrayStatus.CandidateVersion = "candidate"
	summary.GrayStatus.CandidateChecksum = "Authorization: Bearer raw-token"
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "gray_candidate_checksum") {
		t.Fatalf("expected sensitive gray candidate rejection, got %v", err)
	}

	summary = generated
	summary.GrayStatus.HasRollback = false
	summary.GrayStatus.RollbackVersion = ""
	summary.GrayStatus.RollbackChecksum = ""
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "rollback") {
		t.Fatalf("expected missing rollback status rejection, got %v", err)
	}

	summary = generated
	summary.GrayStatus.RollbackVersion = "other-version"
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "rollback") {
		t.Fatalf("expected rollback status mismatch rejection, got %v", err)
	}

	summary = generated
	summary.GrayStatus.HasCandidate = true
	summary.GrayStatus.CandidateVersion = "v3"
	summary.GrayStatus.CandidateChecksum = "sha256:candidate"
	summary.GrayStatus.CandidateGrayPercent = 20
	summary.GrayStatus.CandidateTrafficPercent = 10
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "candidate traffic") {
		t.Fatalf("expected candidate traffic mismatch rejection, got %v", err)
	}

	summary = generated
	summary.GrayStatus.ActiveTrafficPercent = 50
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "active traffic") {
		t.Fatalf("expected no-candidate active traffic rejection, got %v", err)
	}

	summary = generated
	summary.CacheInvalidation.OperationID = "sk-1234567890abcdef"
	if err := summary.Validate(); err == nil ||
		!strings.Contains(err.Error(), "cache_invalidation") {
		t.Fatalf("expected unsafe cache invalidation rejection, got %v", err)
	}
}

func TestNewAppConfigOperationSummaryRequiresRollbackInResultVersions(t *testing.T) {
	input := validAppConfigOperationSummaryInput()
	input.ResultVersions = []AppConfigVersion{input.NextActive}

	if _, err := NewAppConfigOperationSummary(input); err == nil ||
		!strings.Contains(err.Error(), "rollback") {
		t.Fatalf("expected missing rollback result rejection, got %v", err)
	}
}

func validAppConfigOperationSummaryInput() AppConfigOperationSummaryInput {
	previous := validLifecycleConfigVersion("v1", AppConfigVersionStatusRollback)
	previous.Checksum = "sha256:previous"
	next := validLifecycleConfigVersion("v2", AppConfigVersionStatusActive)
	next.Checksum = "sha256:next"
	return AppConfigOperationSummaryInput{
		Operation:      AppConfigOperationActivate,
		PreviousActive: previous,
		NextActive:     next,
		ResultVersions: []AppConfigVersion{previous, next},
		OperationID:    "activate-1",
		TraceID:        "trace",
		CreatedAt:      time.Now(),
	}
}

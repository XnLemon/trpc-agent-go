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

func TestNewBackendMigrationStatusReportBuildsSafeReport(t *testing.T) {
	updatedAt := time.Date(2026, 7, 8, 14, 0, 0, 0, time.UTC)
	input := BackendMigrationStatusInput{
		TenantID:            "tenant",
		AppID:               "app",
		ProfileID:           "profile-a",
		Resource:            BackendMigrationResourceSession,
		SourceBackendID:     "redis-primary",
		TargetBackendID:     "sql-primary",
		MigrationMode:       StorageMigrationModeDualWrite,
		Status:              BackendMigrationStatusVerifying,
		OperationID:         "migration-1",
		SourceRecordCount:   100,
		TargetRecordCount:   97,
		VerifiedRecordCount: 97,
		MismatchCount:       1,
		LastRecordID:        "event-100",
		SampleSetRef:        "sample://tenant/app/session-migration-1",
		SampledTopKQueries:  10,
		MatchedTopKQueries:  9,
		TraceID:             "trace-1",
		UpdatedAt:           updatedAt,
	}

	report, err := NewBackendMigrationStatusReport(input)
	if err != nil {
		t.Fatalf("new backend migration status report: %v", err)
	}
	if report.TenantID != "tenant" || report.AppID != "app" || report.ProfileID != "profile-a" {
		t.Fatalf("unexpected owner/profile: %+v", report)
	}
	if report.Resource != BackendMigrationResourceSession ||
		report.SourceBackendID != "redis-primary" ||
		report.TargetBackendID != "sql-primary" ||
		report.MigrationMode != StorageMigrationModeDualWrite ||
		report.Status != BackendMigrationStatusVerifying {
		t.Fatalf("unexpected routing fields: %+v", report)
	}
	if report.SourceRecordCount != 100 ||
		report.TargetRecordCount != 97 ||
		report.LagRecordCount != 3 ||
		report.VerifiedRecordCount != 97 ||
		report.MismatchCount != 1 {
		t.Fatalf("unexpected count summary: %+v", report)
	}
	if report.SampledTopKQueries != 10 || report.MatchedTopKQueries != 9 {
		t.Fatalf("unexpected topK summary: %+v", report)
	}
	if report.OperationID != "migration-1" || report.TraceID != "trace-1" ||
		!report.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected operation metadata: %+v", report)
	}
	if !strings.HasPrefix(report.MigrationID, backendMigrationIDPrefix) {
		t.Fatalf("unexpected migration id: %q", report.MigrationID)
	}

	again, err := NewBackendMigrationStatusReport(input)
	if err != nil {
		t.Fatalf("new duplicate backend migration status report: %v", err)
	}
	if report.MigrationID != again.MigrationID {
		t.Fatalf("expected stable migration id, got %q and %q", report.MigrationID, again.MigrationID)
	}

	nextOperation := input
	nextOperation.OperationID = "migration-2"
	nextReport, err := NewBackendMigrationStatusReport(nextOperation)
	if err != nil {
		t.Fatalf("new next backend migration status report: %v", err)
	}
	if report.MigrationID == nextReport.MigrationID {
		t.Fatalf("expected operation id to scope migration id, got %q", report.MigrationID)
	}

	serialized := fmt.Sprintf("%+v", report)
	if strings.Contains(serialized, "password=plain") {
		t.Fatalf("report leaked sensitive content: %s", serialized)
	}
}

func TestNewBackendMigrationStatusReportRejectsInvalidInputs(t *testing.T) {
	base := validBackendMigrationStatusInput()

	missingTenant := base
	missingTenant.TenantID = " "
	if _, err := NewBackendMigrationStatusReport(missingTenant); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}

	missingProfile := base
	missingProfile.ProfileID = " "
	if _, err := NewBackendMigrationStatusReport(missingProfile); err == nil ||
		!strings.Contains(err.Error(), "profile_id") {
		t.Fatalf("expected profile id requirement, got %v", err)
	}

	unknownResource := base
	unknownResource.Resource = "cache"
	if _, err := NewBackendMigrationStatusReport(unknownResource); err == nil ||
		!strings.Contains(err.Error(), "invalid backend migration resource") {
		t.Fatalf("expected resource validation, got %v", err)
	}

	sameBackend := base
	sameBackend.TargetBackendID = sameBackend.SourceBackendID
	if _, err := NewBackendMigrationStatusReport(sameBackend); err == nil ||
		!strings.Contains(err.Error(), "must differ") {
		t.Fatalf("expected source/target mismatch requirement, got %v", err)
	}

	normalMode := base
	normalMode.MigrationMode = StorageMigrationModeNormal
	if _, err := NewBackendMigrationStatusReport(normalMode); err == nil ||
		!strings.Contains(err.Error(), "active migration mode") {
		t.Fatalf("expected active migration mode requirement, got %v", err)
	}

	unknownStatus := base
	unknownStatus.Status = "paused"
	if _, err := NewBackendMigrationStatusReport(unknownStatus); err == nil ||
		!strings.Contains(err.Error(), "invalid backend migration status") {
		t.Fatalf("expected status validation, got %v", err)
	}

	missingOperation := base
	missingOperation.OperationID = " "
	if _, err := NewBackendMigrationStatusReport(missingOperation); err == nil ||
		!strings.Contains(err.Error(), "operation_id") {
		t.Fatalf("expected operation id requirement, got %v", err)
	}

	negativeCount := base
	negativeCount.TargetRecordCount = -1
	if _, err := NewBackendMigrationStatusReport(negativeCount); err == nil ||
		!strings.Contains(err.Error(), "target_record_count") {
		t.Fatalf("expected non-negative count validation, got %v", err)
	}

	tooManyMismatches := base
	tooManyMismatches.MismatchCount = tooManyMismatches.VerifiedRecordCount + 1
	if _, err := NewBackendMigrationStatusReport(tooManyMismatches); err == nil ||
		!strings.Contains(err.Error(), "mismatch_count") {
		t.Fatalf("expected mismatch bound validation, got %v", err)
	}

	tooManyTopKMatches := base
	tooManyTopKMatches.MatchedTopKQueries = tooManyTopKMatches.SampledTopKQueries + 1
	if _, err := NewBackendMigrationStatusReport(tooManyTopKMatches); err == nil ||
		!strings.Contains(err.Error(), "matched_topk_queries") {
		t.Fatalf("expected topK bound validation, got %v", err)
	}

	failedWithoutReason := base
	failedWithoutReason.Status = BackendMigrationStatusFailed
	failedWithoutReason.FailureReason = " "
	if _, err := NewBackendMigrationStatusReport(failedWithoutReason); err == nil ||
		!strings.Contains(err.Error(), "failure_reason") {
		t.Fatalf("expected failed status reason requirement, got %v", err)
	}

	sensitiveReason := base
	sensitiveReason.Status = BackendMigrationStatusFailed
	sensitiveReason.FailureReason = "password=plain"
	if _, err := NewBackendMigrationStatusReport(sensitiveReason); err == nil ||
		!strings.Contains(err.Error(), "failure_reason") {
		t.Fatalf("expected sensitive failure reason rejection, got %v", err)
	}

	zeroUpdatedAt := base
	zeroUpdatedAt.UpdatedAt = time.Time{}
	if _, err := NewBackendMigrationStatusReport(zeroUpdatedAt); err == nil ||
		!strings.Contains(err.Error(), "updated_at") {
		t.Fatalf("expected updated at requirement, got %v", err)
	}
}

func TestBackendMigrationStatusReportValidateRejectsUnsafeReport(t *testing.T) {
	generated, err := NewBackendMigrationStatusReport(validBackendMigrationStatusInput())
	if err != nil {
		t.Fatalf("new generated report: %v", err)
	}
	report := generated
	if err := report.Validate(); err != nil {
		t.Fatalf("expected report to validate: %v", err)
	}

	report.MigrationID = "backend_migration_profile-a"
	if err := report.Validate(); err == nil ||
		!strings.Contains(err.Error(), "migration_id") {
		t.Fatalf("expected unsafe migration id rejection, got %v", err)
	}

	report = generated
	report.OperationID = "other-migration"
	if err := report.Validate(); err == nil ||
		!strings.Contains(err.Error(), "migration_id") {
		t.Fatalf("expected stale migration id rejection, got %v", err)
	}

	report = generated
	report.LagRecordCount = -1
	if err := report.Validate(); err == nil ||
		!strings.Contains(err.Error(), "lag_record_count") {
		t.Fatalf("expected unsafe lag count rejection, got %v", err)
	}

	report = generated
	report.LagRecordCount = 0
	if err := report.Validate(); err == nil ||
		!strings.Contains(err.Error(), "lag_record_count") {
		t.Fatalf("expected inconsistent lag count rejection, got %v", err)
	}

	report = generated
	report.FailureReason = "token: plain"
	if err := report.Validate(); err == nil ||
		!strings.Contains(err.Error(), "failure_reason") {
		t.Fatalf("expected sensitive failure reason rejection, got %v", err)
	}
}

func TestNewBackendMigrationStatusReportEnforcesStatusGates(t *testing.T) {
	readyWithLag := validBackendMigrationStatusInput()
	readyWithLag.Status = BackendMigrationStatusReady
	if _, err := NewBackendMigrationStatusReport(readyWithLag); err == nil ||
		!strings.Contains(err.Error(), "source_record_count") {
		t.Fatalf("expected ready status count equality rejection, got %v", err)
	}

	readyWithMismatch := validBackendMigrationStatusInput()
	readyWithMismatch.Status = BackendMigrationStatusReady
	readyWithMismatch.TargetRecordCount = readyWithMismatch.SourceRecordCount
	readyWithMismatch.VerifiedRecordCount = readyWithMismatch.SourceRecordCount
	readyWithMismatch.MismatchCount = 1
	if _, err := NewBackendMigrationStatusReport(readyWithMismatch); err == nil ||
		!strings.Contains(err.Error(), "mismatch_count") {
		t.Fatalf("expected ready status mismatch rejection, got %v", err)
	}

	readyWithMissingVerification := validBackendMigrationStatusInput()
	readyWithMissingVerification.Status = BackendMigrationStatusReady
	readyWithMissingVerification.TargetRecordCount = readyWithMissingVerification.SourceRecordCount
	readyWithMissingVerification.VerifiedRecordCount = 0
	if _, err := NewBackendMigrationStatusReport(readyWithMissingVerification); err == nil ||
		!strings.Contains(err.Error(), "verified_record_count") {
		t.Fatalf("expected ready status verification count rejection, got %v", err)
	}

	readyWithTopKGap := validBackendMigrationStatusInput()
	readyWithTopKGap.Status = BackendMigrationStatusReady
	readyWithTopKGap.TargetRecordCount = readyWithTopKGap.SourceRecordCount
	readyWithTopKGap.VerifiedRecordCount = readyWithTopKGap.SourceRecordCount
	readyWithTopKGap.MatchedTopKQueries = readyWithTopKGap.SampledTopKQueries - 1
	if _, err := NewBackendMigrationStatusReport(readyWithTopKGap); err == nil ||
		!strings.Contains(err.Error(), "topK") {
		t.Fatalf("expected ready status topK rejection, got %v", err)
	}

	completedWrongMode := validBackendMigrationStatusInput()
	completedWrongMode.Status = BackendMigrationStatusCompleted
	completedWrongMode.TargetRecordCount = completedWrongMode.SourceRecordCount
	completedWrongMode.VerifiedRecordCount = completedWrongMode.SourceRecordCount
	if _, err := NewBackendMigrationStatusReport(completedWrongMode); err == nil ||
		!strings.Contains(err.Error(), "cutover") {
		t.Fatalf("expected completed status cutover mode requirement, got %v", err)
	}

	completed := validBackendMigrationStatusInput()
	completed.Status = BackendMigrationStatusCompleted
	completed.MigrationMode = StorageMigrationModeCutover
	completed.TargetRecordCount = completed.SourceRecordCount
	completed.VerifiedRecordCount = completed.SourceRecordCount
	if _, err := NewBackendMigrationStatusReport(completed); err != nil {
		t.Fatalf("expected completed cutover status to validate: %v", err)
	}

	knowledgeWithoutTopKSamples := validBackendMigrationStatusInput()
	knowledgeWithoutTopKSamples.Resource = BackendMigrationResourceKnowledge
	knowledgeWithoutTopKSamples.Status = BackendMigrationStatusReady
	knowledgeWithoutTopKSamples.TargetRecordCount = knowledgeWithoutTopKSamples.SourceRecordCount
	knowledgeWithoutTopKSamples.VerifiedRecordCount = knowledgeWithoutTopKSamples.SourceRecordCount
	knowledgeWithoutTopKSamples.SampledTopKQueries = 0
	knowledgeWithoutTopKSamples.MatchedTopKQueries = 0
	if _, err := NewBackendMigrationStatusReport(knowledgeWithoutTopKSamples); err == nil ||
		!strings.Contains(err.Error(), "sampled_topk_queries") {
		t.Fatalf("expected knowledge topK sample requirement, got %v", err)
	}

	knowledgeWithoutSampleRef := validBackendMigrationStatusInput()
	knowledgeWithoutSampleRef.Resource = BackendMigrationResourceKnowledge
	knowledgeWithoutSampleRef.Status = BackendMigrationStatusReady
	knowledgeWithoutSampleRef.TargetRecordCount = knowledgeWithoutSampleRef.SourceRecordCount
	knowledgeWithoutSampleRef.VerifiedRecordCount = knowledgeWithoutSampleRef.SourceRecordCount
	knowledgeWithoutSampleRef.SampleSetRef = " "
	if _, err := NewBackendMigrationStatusReport(knowledgeWithoutSampleRef); err == nil ||
		!strings.Contains(err.Error(), "sample_set_ref") {
		t.Fatalf("expected knowledge sample ref requirement, got %v", err)
	}

	knowledgeReady := validBackendMigrationStatusInput()
	knowledgeReady.Resource = BackendMigrationResourceKnowledge
	knowledgeReady.Status = BackendMigrationStatusReady
	knowledgeReady.TargetRecordCount = knowledgeReady.SourceRecordCount
	knowledgeReady.VerifiedRecordCount = knowledgeReady.SourceRecordCount
	if _, err := NewBackendMigrationStatusReport(knowledgeReady); err != nil {
		t.Fatalf("expected knowledge ready status to validate: %v", err)
	}

	rolledBackWrongMode := validBackendMigrationStatusInput()
	rolledBackWrongMode.Status = BackendMigrationStatusRolledBack
	if _, err := NewBackendMigrationStatusReport(rolledBackWrongMode); err == nil ||
		!strings.Contains(err.Error(), "rollback") {
		t.Fatalf("expected rolled_back status rollback mode requirement, got %v", err)
	}

	rolledBack := validBackendMigrationStatusInput()
	rolledBack.Status = BackendMigrationStatusRolledBack
	rolledBack.MigrationMode = StorageMigrationModeRollback
	if _, err := NewBackendMigrationStatusReport(rolledBack); err != nil {
		t.Fatalf("expected rolled_back rollback status to validate: %v", err)
	}
}

func TestNewBackendMigrationStatusReportSupportsAcceptanceResources(t *testing.T) {
	for _, resource := range []BackendMigrationResource{
		BackendMigrationResourceSession,
		BackendMigrationResourceSummary,
		BackendMigrationResourceMemory,
		BackendMigrationResourceArtifact,
		BackendMigrationResourceKnowledge,
		BackendMigrationResourceAudit,
	} {
		t.Run(string(resource), func(t *testing.T) {
			input := validBackendMigrationStatusInput()
			input.Resource = resource
			if _, err := NewBackendMigrationStatusReport(input); err != nil {
				t.Fatalf("expected resource %q to validate: %v", resource, err)
			}
		})
	}
}

func validBackendMigrationStatusInput() BackendMigrationStatusInput {
	return BackendMigrationStatusInput{
		TenantID:            "tenant",
		AppID:               "app",
		ProfileID:           "profile",
		Resource:            BackendMigrationResourceSession,
		SourceBackendID:     "redis",
		TargetBackendID:     "sql",
		MigrationMode:       StorageMigrationModeShadowRead,
		Status:              BackendMigrationStatusRunning,
		OperationID:         "migration",
		SourceRecordCount:   20,
		TargetRecordCount:   18,
		VerifiedRecordCount: 18,
		MismatchCount:       0,
		LastRecordID:        "event-20",
		SampleSetRef:        "sample://tenant/app/migration",
		SampledTopKQueries:  5,
		MatchedTopKQueries:  5,
		TraceID:             "trace",
		UpdatedAt:           time.Now(),
	}
}

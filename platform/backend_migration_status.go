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

const backendMigrationIDPrefix = "backend_migration_"

// BackendMigrationResource names the storage resource being migrated.
type BackendMigrationResource string

const (
	// BackendMigrationResourceSession covers session event storage migrations.
	BackendMigrationResourceSession BackendMigrationResource = "session"
	// BackendMigrationResourceSummary covers session summary storage migrations.
	BackendMigrationResourceSummary BackendMigrationResource = "summary"
	// BackendMigrationResourceMemory covers memory store migrations.
	BackendMigrationResourceMemory BackendMigrationResource = "memory"
	// BackendMigrationResourceArtifact covers artifact object storage migrations.
	BackendMigrationResourceArtifact BackendMigrationResource = "artifact"
	// BackendMigrationResourceKnowledge covers knowledge/vector store migrations.
	BackendMigrationResourceKnowledge BackendMigrationResource = "knowledge"
	// BackendMigrationResourceAudit covers audit sink migrations.
	BackendMigrationResourceAudit BackendMigrationResource = "audit"
)

// BackendMigrationStatus describes the lifecycle state of one backend migration task.
type BackendMigrationStatus string

const (
	// BackendMigrationStatusPending means the task is registered but has not started.
	BackendMigrationStatusPending BackendMigrationStatus = "pending"
	// BackendMigrationStatusRunning means records are being copied or dual-written.
	BackendMigrationStatusRunning BackendMigrationStatus = "running"
	// BackendMigrationStatusVerifying means source and target data are being compared.
	BackendMigrationStatusVerifying BackendMigrationStatus = "verifying"
	// BackendMigrationStatusReady means verification is clean enough for cutover.
	BackendMigrationStatusReady BackendMigrationStatus = "ready"
	// BackendMigrationStatusCompleted means cutover completed successfully.
	BackendMigrationStatusCompleted BackendMigrationStatus = "completed"
	// BackendMigrationStatusRolledBack means traffic returned to the source backend.
	BackendMigrationStatusRolledBack BackendMigrationStatus = "rolled_back"
	// BackendMigrationStatusFailed means the task failed before a safe completion.
	BackendMigrationStatusFailed BackendMigrationStatus = "failed"
)

// BackendMigrationStatusInput contains safe metadata for one backend migration update.
type BackendMigrationStatusInput struct {
	TenantID            string
	AppID               string
	ProfileID           string
	Resource            BackendMigrationResource
	SourceBackendID     string
	TargetBackendID     string
	MigrationMode       StorageMigrationMode
	Status              BackendMigrationStatus
	OperationID         string
	SourceRecordCount   int64
	TargetRecordCount   int64
	VerifiedRecordCount int64
	MismatchCount       int64
	LastRecordID        string
	SampleSetRef        string
	SampledTopKQueries  int64
	MatchedTopKQueries  int64
	FailureReason       string
	TraceID             string
	UpdatedAt           time.Time
}

// BackendMigrationStatusReport is a safe operations-facing backend migration task status.
type BackendMigrationStatusReport struct {
	TenantID            string
	AppID               string
	ProfileID           string
	MigrationID         string
	Resource            BackendMigrationResource
	SourceBackendID     string
	TargetBackendID     string
	MigrationMode       StorageMigrationMode
	Status              BackendMigrationStatus
	OperationID         string
	SourceRecordCount   int64
	TargetRecordCount   int64
	LagRecordCount      int64
	VerifiedRecordCount int64
	MismatchCount       int64
	LastRecordID        string
	SampleSetRef        string
	SampledTopKQueries  int64
	MatchedTopKQueries  int64
	FailureReason       string
	TraceID             string
	UpdatedAt           time.Time
}

// NewBackendMigrationStatusReport builds a safe status report for backend migration observability.
func NewBackendMigrationStatusReport(input BackendMigrationStatusInput) (BackendMigrationStatusReport, error) {
	normalized, err := input.normalize()
	if err != nil {
		return BackendMigrationStatusReport{}, err
	}
	report := BackendMigrationStatusReport{
		TenantID:            normalized.TenantID,
		AppID:               normalized.AppID,
		ProfileID:           normalized.ProfileID,
		MigrationID:         normalized.migrationID(),
		Resource:            normalized.Resource,
		SourceBackendID:     normalized.SourceBackendID,
		TargetBackendID:     normalized.TargetBackendID,
		MigrationMode:       normalized.MigrationMode,
		Status:              normalized.Status,
		OperationID:         normalized.OperationID,
		SourceRecordCount:   normalized.SourceRecordCount,
		TargetRecordCount:   normalized.TargetRecordCount,
		LagRecordCount:      backendMigrationLag(normalized.SourceRecordCount, normalized.TargetRecordCount),
		VerifiedRecordCount: normalized.VerifiedRecordCount,
		MismatchCount:       normalized.MismatchCount,
		LastRecordID:        normalized.LastRecordID,
		SampleSetRef:        normalized.SampleSetRef,
		SampledTopKQueries:  normalized.SampledTopKQueries,
		MatchedTopKQueries:  normalized.MatchedTopKQueries,
		FailureReason:       normalized.FailureReason,
		TraceID:             normalized.TraceID,
		UpdatedAt:           normalized.UpdatedAt,
	}
	if err := report.Validate(); err != nil {
		return BackendMigrationStatusReport{}, err
	}
	return report, nil
}

// Validate checks that a backend migration status report is safe to expose or store.
func (r BackendMigrationStatusReport) Validate() error {
	if err := r.validateBackendMigrationIdentity(); err != nil {
		return err
	}
	if err := r.validateBackendMigrationState(); err != nil {
		return err
	}
	return r.validateBackendMigrationSafeText()
}

func (r BackendMigrationStatusReport) validateBackendMigrationIdentity() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(r.ProfileID) == "" {
		return fmt.Errorf("profile_id is required")
	}
	if strings.TrimSpace(r.MigrationID) == "" {
		return fmt.Errorf("migration_id is required")
	}
	if !isBackendMigrationID(r.MigrationID) {
		return fmt.Errorf("migration_id must be %s followed by a 24 character hex hash", backendMigrationIDPrefix)
	}
	if r.MigrationID != backendMigrationID(
		r.TenantID,
		r.AppID,
		r.ProfileID,
		r.Resource,
		r.SourceBackendID,
		r.TargetBackendID,
		r.OperationID,
	) {
		return fmt.Errorf("migration_id does not match backend migration identity")
	}
	if !r.Resource.valid() {
		return fmt.Errorf("invalid backend migration resource %q", r.Resource)
	}
	if strings.TrimSpace(r.SourceBackendID) == "" {
		return fmt.Errorf("source_backend_id is required")
	}
	if strings.TrimSpace(r.TargetBackendID) == "" {
		return fmt.Errorf("target_backend_id is required")
	}
	if strings.TrimSpace(r.SourceBackendID) == strings.TrimSpace(r.TargetBackendID) {
		return fmt.Errorf("source_backend_id and target_backend_id must differ")
	}
	return nil
}

func (r BackendMigrationStatusReport) validateBackendMigrationState() error {
	mode, err := NormalizeStorageMigrationMode(string(r.MigrationMode))
	if err != nil {
		return err
	}
	if !IsActiveStorageMigrationMode(mode) {
		return fmt.Errorf("migration_mode must be an active migration mode")
	}
	if !r.Status.valid() {
		return fmt.Errorf("invalid backend migration status %q", r.Status)
	}
	if strings.TrimSpace(r.OperationID) == "" {
		return fmt.Errorf("operation_id is required")
	}
	if err := validateBackendMigrationCounts(r); err != nil {
		return err
	}
	if err := validateBackendMigrationStatusGate(r); err != nil {
		return err
	}
	if r.Status == BackendMigrationStatusFailed && strings.TrimSpace(r.FailureReason) == "" {
		return fmt.Errorf("failure_reason is required for failed backend migration status")
	}
	if r.UpdatedAt.IsZero() {
		return fmt.Errorf("updated_at is required")
	}
	return nil
}

func (r BackendMigrationStatusReport) validateBackendMigrationSafeText() error {
	for field, value := range map[string]string{
		"app_id":            r.AppID,
		"profile_id":        r.ProfileID,
		"migration_id":      r.MigrationID,
		"source_backend_id": r.SourceBackendID,
		"target_backend_id": r.TargetBackendID,
		"operation_id":      r.OperationID,
		"last_record_id":    r.LastRecordID,
		"sample_set_ref":    r.SampleSetRef,
		"failure_reason":    r.FailureReason,
		"trace_id":          r.TraceID,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return err
		}
	}
	return nil
}

func (i BackendMigrationStatusInput) normalize() (BackendMigrationStatusInput, error) {
	i.TenantID = strings.TrimSpace(i.TenantID)
	if i.TenantID == "" {
		return BackendMigrationStatusInput{}, ErrTenantIDRequired
	}
	i.AppID = strings.TrimSpace(i.AppID)
	i.ProfileID = strings.TrimSpace(i.ProfileID)
	if i.ProfileID == "" {
		return BackendMigrationStatusInput{}, fmt.Errorf("profile_id is required")
	}
	i.Resource = BackendMigrationResource(strings.TrimSpace(string(i.Resource)))
	if !i.Resource.valid() {
		return BackendMigrationStatusInput{}, fmt.Errorf("invalid backend migration resource %q", i.Resource)
	}
	i.SourceBackendID = strings.TrimSpace(i.SourceBackendID)
	if i.SourceBackendID == "" {
		return BackendMigrationStatusInput{}, fmt.Errorf("source_backend_id is required")
	}
	i.TargetBackendID = strings.TrimSpace(i.TargetBackendID)
	if i.TargetBackendID == "" {
		return BackendMigrationStatusInput{}, fmt.Errorf("target_backend_id is required")
	}
	if i.SourceBackendID == i.TargetBackendID {
		return BackendMigrationStatusInput{}, fmt.Errorf("source_backend_id and target_backend_id must differ")
	}
	mode, err := NormalizeStorageMigrationMode(string(i.MigrationMode))
	if err != nil {
		return BackendMigrationStatusInput{}, err
	}
	if !IsActiveStorageMigrationMode(mode) {
		return BackendMigrationStatusInput{}, fmt.Errorf("migration_mode must be an active migration mode")
	}
	i.MigrationMode = mode
	i.Status = BackendMigrationStatus(strings.TrimSpace(string(i.Status)))
	if !i.Status.valid() {
		return BackendMigrationStatusInput{}, fmt.Errorf("invalid backend migration status %q", i.Status)
	}
	i.OperationID = strings.TrimSpace(i.OperationID)
	if i.OperationID == "" {
		return BackendMigrationStatusInput{}, fmt.Errorf("operation_id is required")
	}
	i.LastRecordID = strings.TrimSpace(i.LastRecordID)
	i.SampleSetRef = strings.TrimSpace(i.SampleSetRef)
	i.FailureReason = strings.TrimSpace(i.FailureReason)
	i.TraceID = strings.TrimSpace(i.TraceID)
	report := BackendMigrationStatusReport{
		SourceRecordCount:   i.SourceRecordCount,
		TargetRecordCount:   i.TargetRecordCount,
		LagRecordCount:      backendMigrationLag(i.SourceRecordCount, i.TargetRecordCount),
		VerifiedRecordCount: i.VerifiedRecordCount,
		MismatchCount:       i.MismatchCount,
		SampledTopKQueries:  i.SampledTopKQueries,
		MatchedTopKQueries:  i.MatchedTopKQueries,
	}
	if err := validateBackendMigrationCounts(report); err != nil {
		return BackendMigrationStatusInput{}, err
	}
	report.MigrationMode = i.MigrationMode
	report.Status = i.Status
	if err := validateBackendMigrationStatusGate(report); err != nil {
		return BackendMigrationStatusInput{}, err
	}
	if i.Status == BackendMigrationStatusFailed && i.FailureReason == "" {
		return BackendMigrationStatusInput{}, fmt.Errorf("failure_reason is required for failed backend migration status")
	}
	if i.UpdatedAt.IsZero() {
		return BackendMigrationStatusInput{}, fmt.Errorf("updated_at is required")
	}
	for field, value := range map[string]string{
		"app_id":            i.AppID,
		"profile_id":        i.ProfileID,
		"source_backend_id": i.SourceBackendID,
		"target_backend_id": i.TargetBackendID,
		"operation_id":      i.OperationID,
		"last_record_id":    i.LastRecordID,
		"sample_set_ref":    i.SampleSetRef,
		"failure_reason":    i.FailureReason,
		"trace_id":          i.TraceID,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return BackendMigrationStatusInput{}, err
		}
	}
	return i, nil
}

func (i BackendMigrationStatusInput) migrationID() string {
	return backendMigrationID(
		i.TenantID,
		i.AppID,
		i.ProfileID,
		i.Resource,
		i.SourceBackendID,
		i.TargetBackendID,
		i.OperationID,
	)
}

func backendMigrationID(
	tenantID string,
	appID string,
	profileID string,
	resource BackendMigrationResource,
	sourceBackendID string,
	targetBackendID string,
	operationID string,
) string {
	return backendMigrationIDPrefix + shortHash(
		strings.TrimSpace(tenantID),
		strings.TrimSpace(appID),
		strings.TrimSpace(profileID),
		string(resource),
		strings.TrimSpace(sourceBackendID),
		strings.TrimSpace(targetBackendID),
		strings.TrimSpace(operationID),
	)
}

func validateBackendMigrationCounts(r BackendMigrationStatusReport) error {
	for field, value := range map[string]int64{
		"source_record_count":   r.SourceRecordCount,
		"target_record_count":   r.TargetRecordCount,
		"lag_record_count":      r.LagRecordCount,
		"verified_record_count": r.VerifiedRecordCount,
		"mismatch_count":        r.MismatchCount,
		"sampled_topk_queries":  r.SampledTopKQueries,
		"matched_topk_queries":  r.MatchedTopKQueries,
	} {
		if value < 0 {
			return fmt.Errorf("%s must be non-negative", field)
		}
	}
	if r.MismatchCount > r.VerifiedRecordCount {
		return fmt.Errorf("mismatch_count must be less than or equal to verified_record_count")
	}
	if r.MatchedTopKQueries > r.SampledTopKQueries {
		return fmt.Errorf("matched_topk_queries must be less than or equal to sampled_topk_queries")
	}
	if expected := backendMigrationLag(r.SourceRecordCount, r.TargetRecordCount); r.LagRecordCount != expected {
		return fmt.Errorf("lag_record_count must equal source_record_count minus target_record_count when positive")
	}
	return nil
}

func validateBackendMigrationStatusGate(r BackendMigrationStatusReport) error {
	switch r.Status {
	case BackendMigrationStatusReady, BackendMigrationStatusCompleted:
		if r.SourceRecordCount != r.TargetRecordCount {
			return fmt.Errorf("%s backend migration status requires source_record_count to equal target_record_count", r.Status)
		}
		if r.VerifiedRecordCount != r.SourceRecordCount {
			return fmt.Errorf("%s backend migration status requires verified_record_count to equal source_record_count", r.Status)
		}
		if r.LagRecordCount != 0 {
			return fmt.Errorf("%s backend migration status requires zero lag_record_count", r.Status)
		}
		if r.MismatchCount != 0 {
			return fmt.Errorf("%s backend migration status requires zero mismatch_count", r.Status)
		}
		if r.Resource == BackendMigrationResourceKnowledge && r.SampledTopKQueries == 0 {
			return fmt.Errorf("%s knowledge backend migration status requires sampled_topk_queries", r.Status)
		}
		if r.Resource == BackendMigrationResourceKnowledge && strings.TrimSpace(r.SampleSetRef) == "" {
			return fmt.Errorf("%s knowledge backend migration status requires sample_set_ref", r.Status)
		}
		if r.MatchedTopKQueries != r.SampledTopKQueries {
			return fmt.Errorf("%s backend migration status requires all sampled topK queries to match", r.Status)
		}
	case BackendMigrationStatusRolledBack:
		if r.MigrationMode != StorageMigrationModeRollback {
			return fmt.Errorf("rolled_back backend migration status requires rollback migration_mode")
		}
	}
	if r.Status == BackendMigrationStatusCompleted &&
		r.MigrationMode != StorageMigrationModeCutover {
		return fmt.Errorf("completed backend migration status requires cutover migration_mode")
	}
	return nil
}

func backendMigrationLag(sourceCount, targetCount int64) int64 {
	if sourceCount <= targetCount {
		return 0
	}
	return sourceCount - targetCount
}

func (r BackendMigrationResource) valid() bool {
	switch r {
	case BackendMigrationResourceSession,
		BackendMigrationResourceSummary,
		BackendMigrationResourceMemory,
		BackendMigrationResourceArtifact,
		BackendMigrationResourceKnowledge,
		BackendMigrationResourceAudit:
		return true
	default:
		return false
	}
}

func (s BackendMigrationStatus) valid() bool {
	switch s {
	case BackendMigrationStatusPending,
		BackendMigrationStatusRunning,
		BackendMigrationStatusVerifying,
		BackendMigrationStatusReady,
		BackendMigrationStatusCompleted,
		BackendMigrationStatusRolledBack,
		BackendMigrationStatusFailed:
		return true
	default:
		return false
	}
}

func isBackendMigrationID(value string) bool {
	if !strings.HasPrefix(value, backendMigrationIDPrefix) {
		return false
	}
	return isShortHash(strings.TrimPrefix(value, backendMigrationIDPrefix))
}

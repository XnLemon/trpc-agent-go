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

const configOperationSummaryIDPrefix = "config_operation_"

// AppConfigOperation names an operations-facing config switch.
type AppConfigOperation string

const (
	// AppConfigOperationActivate promotes a released config version to active.
	AppConfigOperationActivate AppConfigOperation = "activate"
	// AppConfigOperationRollback promotes a rollback config version to active.
	AppConfigOperationRollback AppConfigOperation = "rollback"
)

// AppConfigOperationSummaryInput describes one planned or completed config operation.
type AppConfigOperationSummaryInput struct {
	Operation      AppConfigOperation
	PreviousActive AppConfigVersion
	NextActive     AppConfigVersion
	ResultVersions []AppConfigVersion
	OperationID    string
	TraceID        string
	CreatedAt      time.Time
}

// AppConfigOperationSummary is a safe operations-facing summary of one config switch.
type AppConfigOperationSummary struct {
	TenantID           string
	AppID              string
	SummaryID          string
	Operation          AppConfigOperation
	OperationID        string
	PreviousVersion    string
	PreviousChecksum   string
	NextVersion        string
	NextChecksum       string
	DiffChangeCount    int
	CacheInvalidation  AppConfigCacheInvalidation
	GrayStatus         ConfigGrayStatusSummary
	RequiresCacheFlush bool
	TraceID            string
	CreatedAt          time.Time
}

// NewAppConfigOperationSummary builds a safe config operation summary from existing contracts.
func NewAppConfigOperationSummary(input AppConfigOperationSummaryInput) (AppConfigOperationSummary, error) {
	normalized, err := input.normalize()
	if err != nil {
		return AppConfigOperationSummary{}, err
	}
	diff, err := DiffAppConfigVersions(normalized.PreviousActive, normalized.NextActive)
	if err != nil {
		return AppConfigOperationSummary{}, err
	}
	invalidation, err := NewAppConfigCacheInvalidation(AppConfigCacheInvalidationInput{
		PreviousVersion: normalized.PreviousActive,
		NextVersion:     normalized.NextActive,
		Reason:          normalized.invalidationReason(),
		OperationID:     normalized.OperationID,
		TraceID:         normalized.TraceID,
		CreatedAt:       normalized.CreatedAt,
	})
	if err != nil {
		return AppConfigOperationSummary{}, err
	}
	grayStatus, err := SummarizeAppConfigGrayStatus(normalized.ResultVersions)
	if err != nil {
		return AppConfigOperationSummary{}, err
	}
	summary := AppConfigOperationSummary{
		TenantID:           normalized.NextActive.TenantID,
		AppID:              normalized.NextActive.AppID,
		SummaryID:          normalized.summaryID(),
		Operation:          normalized.Operation,
		OperationID:        normalized.OperationID,
		PreviousVersion:    normalized.PreviousActive.Version,
		PreviousChecksum:   normalized.PreviousActive.Checksum,
		NextVersion:        normalized.NextActive.Version,
		NextChecksum:       normalized.NextActive.Checksum,
		DiffChangeCount:    len(diff.Changes),
		CacheInvalidation:  invalidation,
		GrayStatus:         grayStatus,
		RequiresCacheFlush: true,
		TraceID:            normalized.TraceID,
		CreatedAt:          normalized.CreatedAt,
	}
	if err := summary.Validate(); err != nil {
		return AppConfigOperationSummary{}, err
	}
	return summary, nil
}

// Validate checks that a config operation summary is safe to expose or store.
func (s AppConfigOperationSummary) Validate() error {
	if err := s.validateConfigOperationIdentity(); err != nil {
		return err
	}
	if err := s.validateConfigOperationState(); err != nil {
		return err
	}
	if err := s.validateConfigOperationLinks(); err != nil {
		return err
	}
	return s.validateConfigOperationSafeText()
}

func (s AppConfigOperationSummary) validateConfigOperationIdentity() error {
	if strings.TrimSpace(s.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(s.AppID) == "" {
		return ErrAppIDRequired
	}
	if strings.TrimSpace(s.SummaryID) == "" {
		return fmt.Errorf("summary_id is required")
	}
	if !isConfigOperationSummaryID(s.SummaryID) {
		return fmt.Errorf("summary_id must be %s followed by a 24 character hex hash", configOperationSummaryIDPrefix)
	}
	if s.SummaryID != configOperationSummaryID(
		s.TenantID,
		s.AppID,
		s.Operation,
		s.PreviousVersion,
		s.NextVersion,
		s.OperationID,
	) {
		return fmt.Errorf("summary_id does not match config operation identity")
	}
	if !s.Operation.valid() {
		return fmt.Errorf("invalid config operation %q", s.Operation)
	}
	if strings.TrimSpace(s.OperationID) == "" {
		return fmt.Errorf("operation_id is required")
	}
	return nil
}

func (s AppConfigOperationSummary) validateConfigOperationState() error {
	if strings.TrimSpace(s.PreviousVersion) == "" {
		return fmt.Errorf("previous_version is required")
	}
	if strings.TrimSpace(s.NextVersion) == "" {
		return fmt.Errorf("next_version is required")
	}
	if strings.TrimSpace(s.PreviousVersion) == strings.TrimSpace(s.NextVersion) {
		return fmt.Errorf("config operation must change active version")
	}
	if strings.TrimSpace(s.PreviousChecksum) == "" ||
		strings.TrimSpace(s.NextChecksum) == "" {
		return fmt.Errorf("config checksums are required")
	}
	if s.DiffChangeCount <= 0 {
		return fmt.Errorf("diff_change_count must be positive")
	}
	if !s.RequiresCacheFlush {
		return fmt.Errorf("requires_cache_flush must be true")
	}
	if s.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	return nil
}

func (s AppConfigOperationSummary) validateConfigOperationLinks() error {
	if err := s.CacheInvalidation.Validate(); err != nil {
		return fmt.Errorf("cache_invalidation: %w", err)
	}
	if err := validateConfigOperationInvalidation(s); err != nil {
		return err
	}
	if err := validateConfigOperationOwner(s, s.CacheInvalidation.TenantID, s.CacheInvalidation.AppID); err != nil {
		return err
	}
	if err := validateConfigOperationOwner(s, s.GrayStatus.TenantID, s.GrayStatus.AppID); err != nil {
		return err
	}
	if err := validateConfigOperationGrayStatus(s); err != nil {
		return err
	}
	if s.GrayStatus.ActiveVersion != s.NextVersion ||
		s.GrayStatus.ActiveChecksum != s.NextChecksum {
		return fmt.Errorf("gray_status active version must match next active version")
	}
	return nil
}

func (s AppConfigOperationSummary) validateConfigOperationSafeText() error {
	for field, value := range map[string]string{
		"summary_id":        s.SummaryID,
		"operation_id":      s.OperationID,
		"previous_version":  s.PreviousVersion,
		"previous_checksum": s.PreviousChecksum,
		"next_version":      s.NextVersion,
		"next_checksum":     s.NextChecksum,
		"trace_id":          s.TraceID,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigOperationInvalidation(s AppConfigOperationSummary) error {
	expectedReason := AppConfigCacheInvalidationReasonActivate
	if s.Operation == AppConfigOperationRollback {
		expectedReason = AppConfigCacheInvalidationReasonRollback
	}
	if s.CacheInvalidation.Reason != expectedReason {
		return fmt.Errorf("cache_invalidation reason must match config operation")
	}
	if s.CacheInvalidation.OperationID != s.OperationID {
		return fmt.Errorf("cache_invalidation operation_id must match config operation")
	}
	if s.CacheInvalidation.PreviousVersion != s.PreviousVersion ||
		s.CacheInvalidation.PreviousChecksum != s.PreviousChecksum ||
		s.CacheInvalidation.NextVersion != s.NextVersion ||
		s.CacheInvalidation.NextChecksum != s.NextChecksum {
		return fmt.Errorf("cache_invalidation version summary must match config operation")
	}
	if s.CacheInvalidation.TraceID != s.TraceID {
		return fmt.Errorf("cache_invalidation trace_id must match config operation")
	}
	if !s.CacheInvalidation.CreatedAt.Equal(s.CreatedAt) {
		return fmt.Errorf("cache_invalidation created_at must match config operation")
	}
	return nil
}

func validateConfigOperationGrayStatus(s AppConfigOperationSummary) error {
	if err := validateConfigOperationGrayStatusText(s.GrayStatus); err != nil {
		return err
	}
	if s.GrayStatus.ActiveTrafficPercent < 0 || s.GrayStatus.ActiveTrafficPercent > 100 ||
		s.GrayStatus.CandidateGrayPercent < 0 || s.GrayStatus.CandidateGrayPercent > 100 ||
		s.GrayStatus.CandidateTrafficPercent < 0 || s.GrayStatus.CandidateTrafficPercent > 100 {
		return fmt.Errorf("gray_status traffic percentages must be between 0 and 100")
	}
	if err := validateConfigOperationGrayCandidate(s.GrayStatus); err != nil {
		return err
	}
	if !s.GrayStatus.HasRollback {
		return fmt.Errorf("gray_status rollback version is required")
	}
	if s.GrayStatus.RollbackVersion != s.PreviousVersion ||
		s.GrayStatus.RollbackChecksum != s.PreviousChecksum {
		return fmt.Errorf("gray_status rollback version must match previous active version")
	}
	return nil
}

func validateConfigOperationGrayStatusText(status ConfigGrayStatusSummary) error {
	for field, value := range map[string]string{
		"gray_active_version":     status.ActiveVersion,
		"gray_active_checksum":    status.ActiveChecksum,
		"gray_candidate_version":  status.CandidateVersion,
		"gray_candidate_checksum": status.CandidateChecksum,
		"gray_rollback_version":   status.RollbackVersion,
		"gray_rollback_checksum":  status.RollbackChecksum,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigOperationGrayCandidate(status ConfigGrayStatusSummary) error {
	if !status.HasCandidate {
		return validateConfigOperationNoGrayCandidate(status)
	}
	if strings.TrimSpace(status.CandidateVersion) == "" ||
		strings.TrimSpace(status.CandidateChecksum) == "" {
		return fmt.Errorf("gray_status candidate version and checksum are required")
	}
	if status.CandidateGrayPercent != status.CandidateTrafficPercent {
		return fmt.Errorf("gray_status candidate traffic must match candidate gray percent")
	}
	if status.ActiveTrafficPercent != 100-status.CandidateTrafficPercent {
		return fmt.Errorf("gray_status active traffic must complement candidate traffic")
	}
	return nil
}

func validateConfigOperationNoGrayCandidate(status ConfigGrayStatusSummary) error {
	if status.CandidateVersion != "" ||
		status.CandidateChecksum != "" ||
		status.CandidateGrayPercent != 0 ||
		status.CandidateTrafficPercent != 0 {
		return fmt.Errorf("gray_status candidate fields require has_candidate")
	}
	if status.ActiveTrafficPercent != 100 {
		return fmt.Errorf("gray_status active traffic must be 100 when there is no candidate")
	}
	return nil
}

func (i AppConfigOperationSummaryInput) normalize() (AppConfigOperationSummaryInput, error) {
	i.Operation = AppConfigOperation(strings.TrimSpace(string(i.Operation)))
	if !i.Operation.valid() {
		return AppConfigOperationSummaryInput{}, fmt.Errorf("invalid config operation %q", i.Operation)
	}
	if err := i.PreviousActive.Validate(); err != nil {
		return AppConfigOperationSummaryInput{}, fmt.Errorf("previous active config version: %w", err)
	}
	if i.PreviousActive.Status != AppConfigVersionStatusRollback {
		return AppConfigOperationSummaryInput{}, fmt.Errorf("previous active config version status must be rollback")
	}
	if err := i.NextActive.Validate(); err != nil {
		return AppConfigOperationSummaryInput{}, fmt.Errorf("next active config version: %w", err)
	}
	if i.NextActive.Status != AppConfigVersionStatusActive {
		return AppConfigOperationSummaryInput{}, fmt.Errorf("next active config version status must be active")
	}
	if err := requireSameConfigOwner(i.PreviousActive, i.NextActive); err != nil {
		return AppConfigOperationSummaryInput{}, err
	}
	if strings.TrimSpace(i.PreviousActive.Version) == strings.TrimSpace(i.NextActive.Version) {
		return AppConfigOperationSummaryInput{}, fmt.Errorf("config operation must change active version")
	}
	i.OperationID = strings.TrimSpace(i.OperationID)
	if i.OperationID == "" {
		return AppConfigOperationSummaryInput{}, fmt.Errorf("operation_id is required")
	}
	i.TraceID = strings.TrimSpace(i.TraceID)
	if i.CreatedAt.IsZero() {
		return AppConfigOperationSummaryInput{}, fmt.Errorf("created_at is required")
	}
	if len(i.ResultVersions) == 0 {
		return AppConfigOperationSummaryInput{}, fmt.Errorf("result_versions are required")
	}
	for _, version := range i.ResultVersions {
		if err := requireSameConfigOwner(i.NextActive, version); err != nil {
			return AppConfigOperationSummaryInput{}, err
		}
	}
	for field, value := range map[string]string{
		"operation_id": i.OperationID,
		"trace_id":     i.TraceID,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return AppConfigOperationSummaryInput{}, err
		}
	}
	return i, nil
}

func (i AppConfigOperationSummaryInput) invalidationReason() AppConfigCacheInvalidationReason {
	if i.Operation == AppConfigOperationRollback {
		return AppConfigCacheInvalidationReasonRollback
	}
	return AppConfigCacheInvalidationReasonActivate
}

func (i AppConfigOperationSummaryInput) summaryID() string {
	return configOperationSummaryID(
		i.NextActive.TenantID,
		i.NextActive.AppID,
		i.Operation,
		i.PreviousActive.Version,
		i.NextActive.Version,
		i.OperationID,
	)
}

func configOperationSummaryID(
	tenantID string,
	appID string,
	operation AppConfigOperation,
	previousVersion string,
	nextVersion string,
	operationID string,
) string {
	return configOperationSummaryIDPrefix + shortHash(
		strings.TrimSpace(tenantID),
		strings.TrimSpace(appID),
		string(operation),
		strings.TrimSpace(previousVersion),
		strings.TrimSpace(nextVersion),
		strings.TrimSpace(operationID),
	)
}

func isConfigOperationSummaryID(value string) bool {
	if !strings.HasPrefix(value, configOperationSummaryIDPrefix) {
		return false
	}
	return isShortHash(strings.TrimPrefix(value, configOperationSummaryIDPrefix))
}

func validateConfigOperationOwner(s AppConfigOperationSummary, tenantID, appID string) error {
	if strings.TrimSpace(tenantID) != strings.TrimSpace(s.TenantID) {
		return fmt.Errorf("config operation tenant_id must match")
	}
	if strings.TrimSpace(appID) != strings.TrimSpace(s.AppID) {
		return fmt.Errorf("config operation app_id must match")
	}
	return nil
}

func (o AppConfigOperation) valid() bool {
	switch o {
	case AppConfigOperationActivate,
		AppConfigOperationRollback:
		return true
	default:
		return false
	}
}

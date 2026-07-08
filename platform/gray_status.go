//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import "fmt"

// ConfigGrayStatusSummary is an operations-facing view of config gray rollout state.
type ConfigGrayStatusSummary struct {
	TenantID       string
	AppID          string
	ActiveVersion  string
	ActiveChecksum string
	// ActiveTrafficPercent is the configured routing share, not observed live traffic.
	ActiveTrafficPercent int
	HasCandidate         bool
	CandidateVersion     string
	CandidateChecksum    string
	CandidateGrayPercent int
	// CandidateTrafficPercent is the configured routing share, not observed live traffic.
	CandidateTrafficPercent int
	HasRollback             bool
	RollbackVersion         string
	RollbackChecksum        string
}

// SummarizeAppConfigGrayStatus builds a safe gray rollout summary from known config versions.
func SummarizeAppConfigGrayStatus(versions []AppConfigVersion) (ConfigGrayStatusSummary, error) {
	if len(versions) == 0 {
		return ConfigGrayStatusSummary{}, fmt.Errorf("config versions are required")
	}

	var owner AppConfigVersion
	var ownerSet bool
	var active AppConfigVersion
	var hasActive bool
	var candidate AppConfigVersion
	var hasCandidate bool
	var rollback AppConfigVersion
	var hasRollback bool

	for _, version := range versions {
		if err := version.Validate(); err != nil {
			return ConfigGrayStatusSummary{}, fmt.Errorf("config version %q: %w", version.Version, err)
		}
		if !ownerSet {
			owner = version
			ownerSet = true
		} else if err := requireSameConfigOwner(owner, version); err != nil {
			return ConfigGrayStatusSummary{}, err
		}

		switch version.Status {
		case AppConfigVersionStatusActive:
			if hasActive {
				return ConfigGrayStatusSummary{}, fmt.Errorf("multiple active config versions")
			}
			active = version
			hasActive = true
		case AppConfigVersionStatusReleased:
			if hasCandidate {
				return ConfigGrayStatusSummary{}, fmt.Errorf("multiple released config versions")
			}
			candidate = version
			hasCandidate = true
		case AppConfigVersionStatusRollback:
			if hasRollback {
				return ConfigGrayStatusSummary{}, fmt.Errorf("multiple rollback config versions")
			}
			rollback = version
			hasRollback = true
		}
	}
	if !hasActive {
		return ConfigGrayStatusSummary{}, fmt.Errorf("active config version is required")
	}

	summary := ConfigGrayStatusSummary{
		TenantID:             active.TenantID,
		AppID:                active.AppID,
		ActiveVersion:        active.Version,
		ActiveChecksum:       active.Checksum,
		ActiveTrafficPercent: 100,
	}
	if hasCandidate {
		summary.HasCandidate = true
		summary.CandidateVersion = candidate.Version
		summary.CandidateChecksum = candidate.Checksum
		summary.CandidateGrayPercent = candidate.GrayPercent
		summary.CandidateTrafficPercent = candidate.GrayPercent
		summary.ActiveTrafficPercent = 100 - candidate.GrayPercent
	}
	if hasRollback {
		summary.HasRollback = true
		summary.RollbackVersion = rollback.Version
		summary.RollbackChecksum = rollback.Checksum
	}
	return summary, nil
}

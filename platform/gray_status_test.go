//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"strings"
	"testing"
)

func TestSummarizeAppConfigGrayStatusReportsCandidateAndRollback(t *testing.T) {
	active := validGrayActiveConfigVersion()
	active.Checksum = "sha256:active"
	candidate := validGrayCandidateConfigVersion()
	candidate.GrayPercent = 25
	candidate.Checksum = "sha256:candidate"
	rollback := validGrayRollbackConfigVersion()
	rollback.Checksum = "sha256:rollback"
	draft := validAppConfigVersion()
	draft.Version = "draft"
	draft.Status = AppConfigVersionStatusDraft

	summary, err := SummarizeAppConfigGrayStatus([]AppConfigVersion{draft, rollback, candidate, active})
	if err != nil {
		t.Fatalf("summarize gray status: %v", err)
	}
	if summary.TenantID != active.TenantID || summary.AppID != active.AppID {
		t.Fatalf("unexpected owner: %+v", summary)
	}
	if summary.ActiveVersion != "v1" || summary.ActiveChecksum != "sha256:active" {
		t.Fatalf("unexpected active version summary: %+v", summary)
	}
	if summary.ActiveTrafficPercent != 75 {
		t.Fatalf("expected active traffic 75, got %d", summary.ActiveTrafficPercent)
	}
	if !summary.HasCandidate || summary.CandidateVersion != "v2" || summary.CandidateChecksum != "sha256:candidate" {
		t.Fatalf("unexpected candidate summary: %+v", summary)
	}
	if summary.CandidateGrayPercent != 25 || summary.CandidateTrafficPercent != 25 {
		t.Fatalf("unexpected candidate traffic summary: %+v", summary)
	}
	if !summary.HasRollback || summary.RollbackVersion != "v0" || summary.RollbackChecksum != "sha256:rollback" {
		t.Fatalf("unexpected rollback summary: %+v", summary)
	}
}

func TestSummarizeAppConfigGrayStatusHandlesActiveOnly(t *testing.T) {
	active := validGrayActiveConfigVersion()

	summary, err := SummarizeAppConfigGrayStatus([]AppConfigVersion{active})
	if err != nil {
		t.Fatalf("summarize active-only gray status: %v", err)
	}
	if summary.ActiveTrafficPercent != 100 {
		t.Fatalf("expected all traffic on active, got %+v", summary)
	}
	if summary.HasCandidate || summary.CandidateTrafficPercent != 0 || summary.CandidateVersion != "" {
		t.Fatalf("expected no candidate fields, got %+v", summary)
	}
	if summary.HasRollback || summary.RollbackVersion != "" {
		t.Fatalf("expected no rollback fields, got %+v", summary)
	}
}

func TestSummarizeAppConfigGrayStatusRejectsInvalidInputs(t *testing.T) {
	if _, err := SummarizeAppConfigGrayStatus(nil); err == nil ||
		!strings.Contains(err.Error(), "config versions are required") {
		t.Fatalf("expected empty input error, got %v", err)
	}

	invalid := validGrayActiveConfigVersion()
	invalid.ConfigBundleJSON = "{"
	if _, err := SummarizeAppConfigGrayStatus([]AppConfigVersion{invalid}); err == nil ||
		!strings.Contains(err.Error(), "config version") {
		t.Fatalf("expected invalid version error, got %v", err)
	}

	candidate := validGrayCandidateConfigVersion()
	if _, err := SummarizeAppConfigGrayStatus([]AppConfigVersion{candidate}); err == nil ||
		!strings.Contains(err.Error(), "active config version") {
		t.Fatalf("expected missing active error, got %v", err)
	}

	active := validGrayActiveConfigVersion()
	otherActive := validGrayActiveConfigVersion()
	otherActive.Version = "v1b"
	otherActive.Checksum = "sha256:active-b"
	if _, err := SummarizeAppConfigGrayStatus([]AppConfigVersion{active, otherActive}); err == nil ||
		!strings.Contains(err.Error(), "multiple active") {
		t.Fatalf("expected duplicate active error, got %v", err)
	}

	otherCandidate := validGrayCandidateConfigVersion()
	otherCandidate.Version = "v3"
	otherCandidate.Checksum = "sha256:candidate-3"
	if _, err := SummarizeAppConfigGrayStatus([]AppConfigVersion{
		active,
		validGrayCandidateConfigVersion(),
		otherCandidate,
	}); err == nil || !strings.Contains(err.Error(), "multiple released") {
		t.Fatalf("expected duplicate candidate error, got %v", err)
	}

	mismatched := validGrayCandidateConfigVersion()
	mismatched.TenantID = "other-tenant"
	if _, err := SummarizeAppConfigGrayStatus([]AppConfigVersion{active, mismatched}); err == nil ||
		!strings.Contains(err.Error(), "tenant_id") {
		t.Fatalf("expected tenant mismatch error, got %v", err)
	}

	mismatched = validGrayCandidateConfigVersion()
	mismatched.AppID = "other-app"
	if _, err := SummarizeAppConfigGrayStatus([]AppConfigVersion{active, mismatched}); err == nil ||
		!strings.Contains(err.Error(), "app_id") {
		t.Fatalf("expected app mismatch error, got %v", err)
	}

	otherRollback := validGrayRollbackConfigVersion()
	otherRollback.Version = "v-1"
	otherRollback.Checksum = "sha256:rollback-2"
	if _, err := SummarizeAppConfigGrayStatus([]AppConfigVersion{
		active,
		validGrayRollbackConfigVersion(),
		otherRollback,
	}); err == nil || !strings.Contains(err.Error(), "multiple rollback") {
		t.Fatalf("expected duplicate rollback error, got %v", err)
	}
}

func validGrayRollbackConfigVersion() AppConfigVersion {
	version := validAppConfigVersion()
	version.Version = "v0"
	version.Status = AppConfigVersionStatusRollback
	version.GrayPercent = 0
	return version
}

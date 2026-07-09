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
	"time"
)

func TestReleaseAppConfigVersionPromotesValidatedCandidate(t *testing.T) {
	version := validAppConfigVersion()
	version.Status = AppConfigVersionStatusValidated
	version.GrayPercent = 0
	now := time.Now()
	version.ActivatedAt = &now

	released, err := ReleaseAppConfigVersion(version, 25)
	if err != nil {
		t.Fatalf("release config version: %v", err)
	}
	if released.Status != AppConfigVersionStatusReleased {
		t.Fatalf("expected released status, got %q", released.Status)
	}
	if released.GrayPercent != 25 {
		t.Fatalf("expected gray percent 25, got %d", released.GrayPercent)
	}
	if released.ActivatedAt != nil {
		t.Fatalf("released candidate should not carry activated_at")
	}
}

func TestReleaseAppConfigVersionRejectsInvalidTransition(t *testing.T) {
	version := validAppConfigVersion()
	version.Status = AppConfigVersionStatusDraft
	if _, err := ReleaseAppConfigVersion(version, 10); err == nil ||
		!strings.Contains(err.Error(), "validated") {
		t.Fatalf("expected validated status requirement, got %v", err)
	}

	version.Status = AppConfigVersionStatusValidated
	if _, err := ReleaseAppConfigVersion(version, -1); err == nil ||
		!strings.Contains(err.Error(), "gray_percent") {
		t.Fatalf("expected gray percent validation, got %v", err)
	}
	if _, err := ReleaseAppConfigVersion(version, 101); err == nil ||
		!strings.Contains(err.Error(), "gray_percent") {
		t.Fatalf("expected gray percent validation, got %v", err)
	}
}

func TestActivateAppConfigVersionPromotesReleasedAndKeepsRollback(t *testing.T) {
	active := validLifecycleConfigVersion("v1", AppConfigVersionStatusActive)
	released := validLifecycleConfigVersion("v2", AppConfigVersionStatusReleased)
	released.GrayPercent = 50
	activatedAt := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)

	nextActive, rollback, err := ActivateAppConfigVersion(active, released, activatedAt)
	if err != nil {
		t.Fatalf("activate config version: %v", err)
	}
	if nextActive.Version != "v2" || nextActive.Status != AppConfigVersionStatusActive {
		t.Fatalf("expected released version to become active, got %+v", nextActive)
	}
	if nextActive.GrayPercent != 0 {
		t.Fatalf("active version should reset gray percent, got %d", nextActive.GrayPercent)
	}
	if nextActive.ActivatedAt == nil || !nextActive.ActivatedAt.Equal(activatedAt) {
		t.Fatalf("active version should record activation time, got %v", nextActive.ActivatedAt)
	}
	if rollback.Version != "v1" || rollback.Status != AppConfigVersionStatusRollback {
		t.Fatalf("expected previous active to become rollback, got %+v", rollback)
	}
	if rollback.GrayPercent != 0 {
		t.Fatalf("rollback version should not receive gray traffic, got %d", rollback.GrayPercent)
	}
}

func TestActivateAppConfigVersionRejectsInvalidTransitions(t *testing.T) {
	active := validLifecycleConfigVersion("v1", AppConfigVersionStatusReleased)
	released := validLifecycleConfigVersion("v2", AppConfigVersionStatusReleased)
	if _, _, err := ActivateAppConfigVersion(active, released, time.Now()); err == nil ||
		!strings.Contains(err.Error(), "active") {
		t.Fatalf("expected active status requirement, got %v", err)
	}

	active = validLifecycleConfigVersion("v1", AppConfigVersionStatusActive)
	released = validLifecycleConfigVersion("v2", AppConfigVersionStatusValidated)
	if _, _, err := ActivateAppConfigVersion(active, released, time.Now()); err == nil ||
		!strings.Contains(err.Error(), "released") {
		t.Fatalf("expected released status requirement, got %v", err)
	}

	released = validLifecycleConfigVersion("v2", AppConfigVersionStatusReleased)
	released.TenantID = "other-tenant"
	if _, _, err := ActivateAppConfigVersion(active, released, time.Now()); err == nil ||
		!strings.Contains(err.Error(), "tenant_id") {
		t.Fatalf("expected tenant mismatch error, got %v", err)
	}
}

func TestRollbackAppConfigVersionPromotesRollbackAndRetainsCurrent(t *testing.T) {
	active := validLifecycleConfigVersion("v2", AppConfigVersionStatusActive)
	rollback := validLifecycleConfigVersion("v1", AppConfigVersionStatusRollback)
	activatedAt := time.Date(2026, 7, 8, 11, 0, 0, 0, time.UTC)

	nextActive, previousActive, err := RollbackAppConfigVersion(active, rollback, activatedAt)
	if err != nil {
		t.Fatalf("rollback config version: %v", err)
	}
	if nextActive.Version != "v1" || nextActive.Status != AppConfigVersionStatusActive {
		t.Fatalf("expected rollback version to become active, got %+v", nextActive)
	}
	if nextActive.ActivatedAt == nil || !nextActive.ActivatedAt.Equal(activatedAt) {
		t.Fatalf("rollback activation should record activation time, got %v", nextActive.ActivatedAt)
	}
	if previousActive.Version != "v2" || previousActive.Status != AppConfigVersionStatusRollback {
		t.Fatalf("expected replaced active to become rollback, got %+v", previousActive)
	}
}

func TestRollbackAppConfigVersionRejectsInvalidTransitions(t *testing.T) {
	active := validLifecycleConfigVersion("v2", AppConfigVersionStatusReleased)
	rollback := validLifecycleConfigVersion("v1", AppConfigVersionStatusRollback)
	if _, _, err := RollbackAppConfigVersion(active, rollback, time.Now()); err == nil ||
		!strings.Contains(err.Error(), "active") {
		t.Fatalf("expected active status requirement, got %v", err)
	}

	active = validLifecycleConfigVersion("v2", AppConfigVersionStatusActive)
	rollback = validLifecycleConfigVersion("v1", AppConfigVersionStatusReleased)
	if _, _, err := RollbackAppConfigVersion(active, rollback, time.Now()); err == nil ||
		!strings.Contains(err.Error(), "rollback") {
		t.Fatalf("expected rollback status requirement, got %v", err)
	}

	rollback = validLifecycleConfigVersion("v1", AppConfigVersionStatusRollback)
	rollback.AppID = "other-app"
	if _, _, err := RollbackAppConfigVersion(active, rollback, time.Now()); err == nil ||
		!strings.Contains(err.Error(), "app_id") {
		t.Fatalf("expected app mismatch error, got %v", err)
	}
}

func validLifecycleConfigVersion(version string, status AppConfigVersionStatus) AppConfigVersion {
	configVersion := validAppConfigVersion()
	configVersion.Version = version
	configVersion.Status = status
	return configVersion
}

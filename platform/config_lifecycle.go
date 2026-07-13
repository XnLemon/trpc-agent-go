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

// ReleaseAppConfigVersion promotes a validated app config version to gray-release candidate status.
func ReleaseAppConfigVersion(version AppConfigVersion, grayPercent int) (AppConfigVersion, error) {
	if err := version.Validate(); err != nil {
		return AppConfigVersion{}, err
	}
	if version.Status != AppConfigVersionStatusValidated {
		return AppConfigVersion{}, fmt.Errorf("config version status must be validated before release")
	}
	if grayPercent < 0 || grayPercent > 100 {
		return AppConfigVersion{}, fmt.Errorf("gray_percent must be between 0 and 100")
	}
	version.Status = AppConfigVersionStatusReleased
	version.GrayPercent = grayPercent
	version.ActivatedAt = nil
	return version, nil
}

// ActivateAppConfigVersion makes a released app config version the active version and retains the previous active version for rollback.
func ActivateAppConfigVersion(active, released AppConfigVersion, activatedAt time.Time) (AppConfigVersion, AppConfigVersion, error) {
	if err := active.Validate(); err != nil {
		return AppConfigVersion{}, AppConfigVersion{}, fmt.Errorf("active config version: %w", err)
	}
	if active.Status != AppConfigVersionStatusActive {
		return AppConfigVersion{}, AppConfigVersion{}, fmt.Errorf("active config version status must be active")
	}
	if err := released.Validate(); err != nil {
		return AppConfigVersion{}, AppConfigVersion{}, fmt.Errorf("released config version: %w", err)
	}
	if released.Status != AppConfigVersionStatusReleased {
		return AppConfigVersion{}, AppConfigVersion{}, fmt.Errorf("released config version status must be released")
	}
	if err := requireSameConfigOwner(active, released); err != nil {
		return AppConfigVersion{}, AppConfigVersion{}, err
	}

	rollback := active
	rollback.Status = AppConfigVersionStatusRollback
	rollback.GrayPercent = 0

	nextActive := released
	nextActive.Status = AppConfigVersionStatusActive
	nextActive.GrayPercent = 0
	nextActive.ActivatedAt = &activatedAt
	return nextActive, rollback, nil
}

// RollbackAppConfigVersion makes a rollback version active and retains the replaced version as rollback.
func RollbackAppConfigVersion(active, rollback AppConfigVersion, activatedAt time.Time) (AppConfigVersion, AppConfigVersion, error) {
	if err := active.Validate(); err != nil {
		return AppConfigVersion{}, AppConfigVersion{}, fmt.Errorf("active config version: %w", err)
	}
	if active.Status != AppConfigVersionStatusActive {
		return AppConfigVersion{}, AppConfigVersion{}, fmt.Errorf("active config version status must be active")
	}
	if err := rollback.Validate(); err != nil {
		return AppConfigVersion{}, AppConfigVersion{}, fmt.Errorf("rollback config version: %w", err)
	}
	if rollback.Status != AppConfigVersionStatusRollback {
		return AppConfigVersion{}, AppConfigVersion{}, fmt.Errorf("rollback config version status must be rollback")
	}
	if err := requireSameConfigOwner(active, rollback); err != nil {
		return AppConfigVersion{}, AppConfigVersion{}, err
	}

	previousActive := active
	previousActive.Status = AppConfigVersionStatusRollback
	previousActive.GrayPercent = 0

	nextActive := rollback
	nextActive.Status = AppConfigVersionStatusActive
	nextActive.GrayPercent = 0
	nextActive.ActivatedAt = &activatedAt
	return nextActive, previousActive, nil
}

func requireSameConfigOwner(left, right AppConfigVersion) error {
	if strings.TrimSpace(left.TenantID) != strings.TrimSpace(right.TenantID) {
		return fmt.Errorf("config version tenant_id must match")
	}
	if strings.TrimSpace(left.AppID) != strings.TrimSpace(right.AppID) {
		return fmt.Errorf("config version app_id must match")
	}
	return nil
}

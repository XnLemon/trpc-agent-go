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
	"hash/fnv"
	"strings"
)

// ConfigVersionSelection is the deterministic routing decision for one session.
type ConfigVersionSelection struct {
	Version     AppConfigVersion
	Bucket      int
	InCandidate bool
}

// SessionGrayBucket returns the stable 0-99 release bucket for one session.
func SessionGrayBucket(tenantID, appID, sessionID string) (int, error) {
	tenantID = strings.TrimSpace(tenantID)
	appID = strings.TrimSpace(appID)
	sessionID = strings.TrimSpace(sessionID)
	if tenantID == "" {
		return 0, ErrTenantIDRequired
	}
	if appID == "" {
		return 0, ErrAppIDRequired
	}
	if sessionID == "" {
		return 0, fmt.Errorf("session_id is required")
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(tenantID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(appID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(sessionID))
	return int(h.Sum32() % 100), nil
}

// SessionInGrayRelease reports whether one session belongs to the app's gray release.
func SessionInGrayRelease(app AgentApp, sessionID string) (bool, int, error) {
	if err := app.Validate(); err != nil {
		return false, 0, err
	}
	bucket, err := SessionGrayBucket(app.TenantID, app.AppID, sessionID)
	if err != nil {
		return false, 0, err
	}
	return bucket < app.GrayPercent, bucket, nil
}

// SelectAppConfigVersionForSession chooses the active or released gray config version for one session.
func SelectAppConfigVersionForSession(active, candidate AppConfigVersion, sessionID string) (ConfigVersionSelection, error) {
	var selection ConfigVersionSelection
	if err := active.Validate(); err != nil {
		return selection, fmt.Errorf("active config version: %w", err)
	}
	if active.Status != AppConfigVersionStatusActive {
		return selection, fmt.Errorf("active config version status must be active")
	}
	if err := candidate.Validate(); err != nil {
		return selection, fmt.Errorf("candidate config version: %w", err)
	}
	if candidate.Status != AppConfigVersionStatusReleased {
		return selection, fmt.Errorf("candidate config version status must be released")
	}
	if strings.TrimSpace(active.TenantID) != strings.TrimSpace(candidate.TenantID) {
		return selection, fmt.Errorf("candidate tenant_id must match active tenant_id")
	}
	if strings.TrimSpace(active.AppID) != strings.TrimSpace(candidate.AppID) {
		return selection, fmt.Errorf("candidate app_id must match active app_id")
	}

	bucket, err := SessionGrayBucket(active.TenantID, active.AppID, sessionID)
	if err != nil {
		return selection, err
	}
	if bucket < candidate.GrayPercent {
		return ConfigVersionSelection{Version: candidate, Bucket: bucket, InCandidate: true}, nil
	}
	return ConfigVersionSelection{Version: active, Bucket: bucket}, nil
}

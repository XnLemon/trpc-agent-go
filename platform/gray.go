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

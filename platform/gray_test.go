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
	"testing"
)

func TestSessionGrayBucketIsStableByTenantAppSession(t *testing.T) {
	first, err := SessionGrayBucket("tenant-a", "app-a", "session-1")
	if err != nil {
		t.Fatalf("bucket: %v", err)
	}
	second, err := SessionGrayBucket(" tenant-a ", " app-a ", " session-1 ")
	if err != nil {
		t.Fatalf("bucket: %v", err)
	}
	if first != second {
		t.Fatalf("same trimmed session key should stay in same bucket: %d != %d", first, second)
	}
	if first < 0 || first >= 100 {
		t.Fatalf("bucket should be 0-99, got %d", first)
	}
}

func TestSessionInGrayReleaseUsesConfiguredPercentBoundary(t *testing.T) {
	app := AgentApp{TenantID: "tenant-a", AppID: "app-a", GrayPercent: 0}
	inGray, bucket, err := SessionInGrayRelease(app, "session-1")
	if err != nil {
		t.Fatalf("gray decision: %v", err)
	}
	if inGray {
		t.Fatalf("0 percent should not include bucket %d", bucket)
	}

	app.GrayPercent = 100
	inGray, bucket, err = SessionInGrayRelease(app, "session-1")
	if err != nil {
		t.Fatalf("gray decision: %v", err)
	}
	if !inGray {
		t.Fatalf("100 percent should include bucket %d", bucket)
	}
}

func TestSessionInGrayReleaseMatchesBucketThreshold(t *testing.T) {
	app := AgentApp{TenantID: "tenant-a", AppID: "app-a", GrayPercent: 50}
	inGray, bucket, err := SessionInGrayRelease(app, "session-1")
	if err != nil {
		t.Fatalf("gray decision: %v", err)
	}
	if inGray != (bucket < app.GrayPercent) {
		t.Fatalf("gray decision should match bucket threshold: in_gray=%t bucket=%d percent=%d", inGray, bucket, app.GrayPercent)
	}
}

func TestSessionInGrayReleaseRejectsInvalidInputs(t *testing.T) {
	_, _, err := SessionInGrayRelease(AgentApp{AppID: "app", GrayPercent: 10}, "session")
	if !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant id error, got %v", err)
	}
	_, _, err = SessionInGrayRelease(AgentApp{TenantID: "tenant", AppID: "app", GrayPercent: 101}, "session")
	if err == nil {
		t.Fatalf("expected invalid gray percent error")
	}
	_, _, err = SessionInGrayRelease(AgentApp{TenantID: "tenant", AppID: "app", GrayPercent: 10}, "")
	if err == nil {
		t.Fatalf("expected missing session id error")
	}
}

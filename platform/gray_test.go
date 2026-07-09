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

func TestSelectAppConfigVersionForSessionIsStable(t *testing.T) {
	active := validGrayActiveConfigVersion()
	candidate := validGrayCandidateConfigVersion()
	candidate.GrayPercent = 100

	first, err := SelectAppConfigVersionForSession(active, candidate, "session-1")
	if err != nil {
		t.Fatalf("select config version: %v", err)
	}
	second, err := SelectAppConfigVersionForSession(active, candidate, " session-1 ")
	if err != nil {
		t.Fatalf("select config version: %v", err)
	}
	if first.Bucket != second.Bucket {
		t.Fatalf("same session should use stable bucket: %d != %d", first.Bucket, second.Bucket)
	}
	if !first.InCandidate || first.Version.Version != candidate.Version {
		t.Fatalf("expected candidate version, got %+v", first)
	}
}

func TestSelectAppConfigVersionForSessionUsesGrayPercentBoundaries(t *testing.T) {
	active := validGrayActiveConfigVersion()
	candidate := validGrayCandidateConfigVersion()

	candidate.GrayPercent = 0
	selection, err := SelectAppConfigVersionForSession(active, candidate, "session-1")
	if err != nil {
		t.Fatalf("select config version: %v", err)
	}
	if selection.InCandidate || selection.Version.Version != active.Version {
		t.Fatalf("0 percent should choose active, got %+v", selection)
	}

	candidate.GrayPercent = 100
	selection, err = SelectAppConfigVersionForSession(active, candidate, "session-1")
	if err != nil {
		t.Fatalf("select config version: %v", err)
	}
	if !selection.InCandidate || selection.Version.Version != candidate.Version {
		t.Fatalf("100 percent should choose candidate, got %+v", selection)
	}
}

func TestSelectAppConfigVersionForSessionMatchesBucketThreshold(t *testing.T) {
	active := validGrayActiveConfigVersion()
	candidate := validGrayCandidateConfigVersion()
	candidate.GrayPercent = 50

	selection, err := SelectAppConfigVersionForSession(active, candidate, "session-1")
	if err != nil {
		t.Fatalf("select config version: %v", err)
	}
	if selection.InCandidate != (selection.Bucket < candidate.GrayPercent) {
		t.Fatalf("selection should match bucket threshold: in_candidate=%t bucket=%d percent=%d",
			selection.InCandidate, selection.Bucket, candidate.GrayPercent)
	}
	if selection.InCandidate && selection.Version.Version != candidate.Version {
		t.Fatalf("candidate hit should return candidate version, got %+v", selection)
	}
	if !selection.InCandidate && selection.Version.Version != active.Version {
		t.Fatalf("active hit should return active version, got %+v", selection)
	}
}

func TestSelectAppConfigVersionForSessionRejectsMismatchedIdentity(t *testing.T) {
	active := validGrayActiveConfigVersion()
	candidate := validGrayCandidateConfigVersion()
	candidate.TenantID = "other-tenant"
	if _, err := SelectAppConfigVersionForSession(active, candidate, "session-1"); err == nil {
		t.Fatalf("expected tenant mismatch error")
	}

	candidate = validGrayCandidateConfigVersion()
	candidate.AppID = "other-app"
	if _, err := SelectAppConfigVersionForSession(active, candidate, "session-1"); err == nil {
		t.Fatalf("expected app mismatch error")
	}
}

func TestSelectAppConfigVersionForSessionRejectsInvalidStatuses(t *testing.T) {
	active := validGrayActiveConfigVersion()
	candidate := validGrayCandidateConfigVersion()

	active.Status = AppConfigVersionStatusReleased
	if _, err := SelectAppConfigVersionForSession(active, candidate, "session-1"); err == nil {
		t.Fatalf("expected active status error")
	}

	active = validGrayActiveConfigVersion()
	candidate.Status = AppConfigVersionStatusValidated
	if _, err := SelectAppConfigVersionForSession(active, candidate, "session-1"); err == nil {
		t.Fatalf("expected candidate status error")
	}
}

func TestSelectAppConfigVersionForSessionRejectsMissingSession(t *testing.T) {
	active := validGrayActiveConfigVersion()
	candidate := validGrayCandidateConfigVersion()
	if _, err := SelectAppConfigVersionForSession(active, candidate, " "); err == nil {
		t.Fatalf("expected missing session id error")
	}
}

func validGrayActiveConfigVersion() AppConfigVersion {
	version := validAppConfigVersion()
	version.Version = "v1"
	version.Status = AppConfigVersionStatusActive
	version.GrayPercent = 0
	return version
}

func validGrayCandidateConfigVersion() AppConfigVersion {
	version := validAppConfigVersion()
	version.Version = "v2"
	version.Status = AppConfigVersionStatusReleased
	version.GrayPercent = 10
	return version
}

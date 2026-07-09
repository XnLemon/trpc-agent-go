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
	"strings"
	"testing"
)

func TestAppConfigVersionValidateAcceptsValidVersion(t *testing.T) {
	version := validAppConfigVersion()
	version.ConfigBundleJSON = `{"model_profile_id":"model","tool_policy_id":"tools","api_key_ref":"secret://model-key"}`

	if err := version.Validate(); err != nil {
		t.Fatalf("expected valid config version, got %v", err)
	}
}

func TestAppConfigVersionValidateRequiresIdentity(t *testing.T) {
	version := validAppConfigVersion()
	version.TenantID = " "
	if err := version.Validate(); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}

	version = validAppConfigVersion()
	version.AppID = " "
	if err := version.Validate(); !errors.Is(err, ErrAppIDRequired) {
		t.Fatalf("expected app requirement, got %v", err)
	}

	version = validAppConfigVersion()
	version.Version = " "
	if err := version.Validate(); err == nil || !strings.Contains(err.Error(), "version is required") {
		t.Fatalf("expected version requirement, got %v", err)
	}
}

func TestAppConfigVersionValidateRequiresBundleAndChecksum(t *testing.T) {
	version := validAppConfigVersion()
	version.ConfigBundleJSON = " "
	if err := version.Validate(); err == nil || !strings.Contains(err.Error(), "config_bundle_json") {
		t.Fatalf("expected config bundle requirement, got %v", err)
	}

	version = validAppConfigVersion()
	version.ConfigBundleJSON = `{"model_profile_id":`
	if err := version.Validate(); err == nil || !strings.Contains(err.Error(), "valid json") {
		t.Fatalf("expected config bundle json validation, got %v", err)
	}

	version = validAppConfigVersion()
	version.Checksum = " "
	if err := version.Validate(); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum requirement, got %v", err)
	}
}

func TestAppConfigVersionValidateRejectsUnsafeBundle(t *testing.T) {
	version := validAppConfigVersion()
	version.ConfigBundleJSON = `{"api_key":"sk-1234567890abcdef"}`

	if err := version.Validate(); err == nil || !strings.Contains(err.Error(), "config_bundle_json") {
		t.Fatalf("expected sensitive bundle rejection, got %v", err)
	}
}

func TestAppConfigVersionValidateRejectsUnsafeRefValue(t *testing.T) {
	version := validAppConfigVersion()
	version.ConfigBundleJSON = `{"api_key_ref":"sk-1234567890abcdef"}`

	if err := version.Validate(); !errors.Is(err, ErrInlineSecretRejected) {
		t.Fatalf("expected inline secret reference rejection, got %v", err)
	}
}

func TestAppConfigVersionValidateRejectsInvalidStatusAndGrayPercent(t *testing.T) {
	version := validAppConfigVersion()
	version.Status = ""
	if err := version.Validate(); err == nil || !strings.Contains(err.Error(), "status is required") {
		t.Fatalf("expected status requirement, got %v", err)
	}

	version = validAppConfigVersion()
	version.Status = AppConfigVersionStatus("paused")
	if err := version.Validate(); err == nil || !strings.Contains(err.Error(), "invalid app config version status") {
		t.Fatalf("expected invalid status, got %v", err)
	}

	version = validAppConfigVersion()
	version.GrayPercent = 101
	if err := version.Validate(); err == nil || !strings.Contains(err.Error(), "gray_percent") {
		t.Fatalf("expected gray percent validation, got %v", err)
	}
}

func validAppConfigVersion() AppConfigVersion {
	return AppConfigVersion{
		TenantID:         "tenant",
		AppID:            "app",
		Version:          "v1",
		ConfigBundleJSON: `{"model_profile_id":"model","tool_policy_id":"tools"}`,
		Checksum:         "sha256:0123456789abcdef",
		Status:           AppConfigVersionStatusDraft,
		GrayPercent:      10,
		CreatedBy:        "operator",
	}
}

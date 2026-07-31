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

func TestDiffAppConfigVersionsReportsMetadataAndBundleChanges(t *testing.T) {
	from := validAppConfigVersion()
	from.Version = "v1"
	from.Checksum = "sha256:1111"
	from.Status = AppConfigVersionStatusActive
	from.GrayPercent = 0
	from.ConfigBundleJSON = `{
		"model_profile_id":"model-a",
		"tool_policy_id":"tools-a",
		"limits":{"max_tokens":1000,"temperature":0.2},
		"tools":["search","ticket"],
		"old_field":"removed"
	}`
	to := validAppConfigVersion()
	to.Version = "v2"
	to.Checksum = "sha256:2222"
	to.Status = AppConfigVersionStatusReleased
	to.GrayPercent = 25
	to.ConfigBundleJSON = `{
		"model_profile_id":"model-b",
		"tool_policy_id":"tools-a",
		"limits":{"max_tokens":2000,"temperature":0.2},
		"tools":["search","crm"],
		"new_field":"added"
	}`

	diff, err := DiffAppConfigVersions(from, to)
	if err != nil {
		t.Fatalf("diff config versions: %v", err)
	}
	if diff.TenantID != "tenant" || diff.AppID != "app" || diff.FromVersion != "v1" || diff.ToVersion != "v2" {
		t.Fatalf("unexpected diff identity: %+v", diff)
	}
	assertConfigDiffChange(t, diff, "version", AppConfigVersionDiffChanged, "v1", "v2")
	assertConfigDiffChange(t, diff, "checksum", AppConfigVersionDiffChanged, "sha256:1111", "sha256:2222")
	assertConfigDiffChange(t, diff, "status", AppConfigVersionDiffChanged, "active", "released")
	assertConfigDiffChange(t, diff, "gray_percent", AppConfigVersionDiffChanged, "0", "25")
	assertConfigDiffChange(t, diff, "/config_bundle_json/model_profile_id", AppConfigVersionDiffChanged, `"model-a"`, `"model-b"`)
	assertConfigDiffChange(t, diff, "/config_bundle_json/limits/max_tokens", AppConfigVersionDiffChanged, "1000", "2000")
	assertConfigDiffChange(t, diff, "/config_bundle_json/tools/1", AppConfigVersionDiffChanged, `"ticket"`, `"crm"`)
	assertConfigDiffChange(t, diff, "/config_bundle_json/old_field", AppConfigVersionDiffRemoved, `"removed"`, "")
	assertConfigDiffChange(t, diff, "/config_bundle_json/new_field", AppConfigVersionDiffAdded, "", `"added"`)
}

func TestDiffAppConfigVersionsReturnsNoChangesForEquivalentBundles(t *testing.T) {
	from := validAppConfigVersion()
	from.ConfigBundleJSON = `{"tool_policy_id":"tools","model_profile_id":"model"}`
	to := from
	to.ConfigBundleJSON = `{
		"model_profile_id":"model",
		"tool_policy_id":"tools"
	}`

	diff, err := DiffAppConfigVersions(from, to)
	if err != nil {
		t.Fatalf("diff equivalent config versions: %v", err)
	}
	if len(diff.Changes) != 0 {
		t.Fatalf("expected no changes, got %+v", diff.Changes)
	}
}

func TestDiffAppConfigVersionsRejectsInvalidInputs(t *testing.T) {
	from := validAppConfigVersion()
	to := validAppConfigVersion()
	to.TenantID = "other-tenant"
	if _, err := DiffAppConfigVersions(from, to); err == nil || !strings.Contains(err.Error(), "tenant_id") {
		t.Fatalf("expected tenant mismatch error, got %v", err)
	}

	to = validAppConfigVersion()
	to.AppID = "other-app"
	if _, err := DiffAppConfigVersions(from, to); err == nil || !strings.Contains(err.Error(), "app_id") {
		t.Fatalf("expected app mismatch error, got %v", err)
	}

	to = validAppConfigVersion()
	to.ConfigBundleJSON = `{"api_key":"sk-1234567890abcdef"}`
	if _, err := DiffAppConfigVersions(from, to); err == nil || !strings.Contains(err.Error(), "to config version") {
		t.Fatalf("expected invalid target validation error, got %v", err)
	}
}

func TestDiffAppConfigVersionsRedactsSensitiveNamedBundleChanges(t *testing.T) {
	from := validAppConfigVersion()
	from.ConfigBundleJSON = `{
		"api_key_ref":"secret://model/old",
		"channel":{"token_ref":"secret://im/old"},
		"headers":{"authorization_ref":"secret://header/old"},
		"nested":{"cookie_ref":"secret://cookie/old"},
		"safe_value":"old"
	}`
	to := validAppConfigVersion()
	to.Version = "v2"
	to.Checksum = "sha256:2222"
	to.ConfigBundleJSON = `{
		"api_key_ref":"secret://model/new",
		"channel":{"token_ref":"secret://im/new"},
		"headers":{"authorization_ref":"secret://header/new"},
		"nested":{"cookie_ref":"secret://cookie/new"},
		"password_ref":"secret://db/new",
		"safe_value":"new"
	}`

	diff, err := DiffAppConfigVersions(from, to)
	if err != nil {
		t.Fatalf("diff sensitive named config bundle changes: %v", err)
	}
	assertConfigDiffChange(t, diff, "/config_bundle_json/api_key_ref", AppConfigVersionDiffChanged, redactedConfigBundleDiffValue, redactedConfigBundleDiffValue)
	assertConfigDiffChange(t, diff, "/config_bundle_json/channel/token_ref", AppConfigVersionDiffChanged, redactedConfigBundleDiffValue, redactedConfigBundleDiffValue)
	assertConfigDiffChange(t, diff, "/config_bundle_json/headers/authorization_ref", AppConfigVersionDiffChanged, redactedConfigBundleDiffValue, redactedConfigBundleDiffValue)
	assertConfigDiffChange(t, diff, "/config_bundle_json/nested/cookie_ref", AppConfigVersionDiffChanged, redactedConfigBundleDiffValue, redactedConfigBundleDiffValue)
	assertConfigDiffChange(t, diff, "/config_bundle_json/password_ref", AppConfigVersionDiffAdded, "", redactedConfigBundleDiffValue)
	assertConfigDiffChange(t, diff, "/config_bundle_json/safe_value", AppConfigVersionDiffChanged, `"old"`, `"new"`)

	serialized := strings.Join(configDiffChangeValues(diff), " ")
	for _, secret := range []string{
		"secret://model/old",
		"secret://model/new",
		"secret://im/old",
		"secret://im/new",
		"secret://header/old",
		"secret://header/new",
		"secret://cookie/old",
		"secret://cookie/new",
		"secret://db/new",
	} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("diff leaked sensitive config value %q in %s", secret, serialized)
		}
	}
}

func TestDiffAppConfigVersionsRedactsNestedSensitiveBundleChanges(t *testing.T) {
	from := validAppConfigVersion()
	from.ConfigBundleJSON = `{
		"removed_parent":{
			"label":"old",
			"channel":{"token_ref":"secret://im/old"}
		},
		"changed_parent":{
			"label":"old",
			"credentials":[{"api_key_ref":"secret://model/old"}]
		}
	}`
	to := validAppConfigVersion()
	to.Version = "v2"
	to.Checksum = "sha256:2222"
	to.ConfigBundleJSON = `{
		"added_parent":{
			"label":"new",
			"channel":{"token_ref":"secret://im/new"}
		},
		"changed_parent":"manual-override"
	}`

	diff, err := DiffAppConfigVersions(from, to)
	if err != nil {
		t.Fatalf("diff nested sensitive config bundle changes: %v", err)
	}

	assertConfigDiffChange(t, diff, "/config_bundle_json/added_parent", AppConfigVersionDiffAdded, "", `{"channel":{"token_ref":"***REDACTED***"},"label":"new"}`)
	assertConfigDiffChange(t, diff, "/config_bundle_json/removed_parent", AppConfigVersionDiffRemoved, `{"channel":{"token_ref":"***REDACTED***"},"label":"old"}`, "")
	assertConfigDiffChange(t, diff, "/config_bundle_json/changed_parent", AppConfigVersionDiffChanged, `{"credentials":"***REDACTED***","label":"old"}`, `"manual-override"`)

	serialized := strings.Join(configDiffChangeValues(diff), " ")
	for _, secret := range []string{
		"secret://im/old",
		"secret://im/new",
		"secret://model/old",
	} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("diff leaked nested sensitive config value %q in %s", secret, serialized)
		}
	}
}

func TestDecodeConfigBundleRejectsTrailingJSON(t *testing.T) {
	_, err := decodeConfigBundle(`{"model_profile_id":"model"} {"tool_policy_id":"tools"}`)

	if err == nil || !strings.Contains(err.Error(), "trailing json") {
		t.Fatalf("expected trailing json rejection, got %v", err)
	}
}

func TestDiffAppConfigVersionsReportsArrayAddRemove(t *testing.T) {
	from := validAppConfigVersion()
	from.ConfigBundleJSON = `{"tools":["search","ticket"]}`
	to := validAppConfigVersion()
	to.Version = "v2"
	to.Checksum = "sha256:2222"
	to.ConfigBundleJSON = `{"tools":["search","ticket","crm"]}`

	diff, err := DiffAppConfigVersions(from, to)
	if err != nil {
		t.Fatalf("diff array growth: %v", err)
	}
	assertConfigDiffChange(t, diff, "/config_bundle_json/tools/2", AppConfigVersionDiffAdded, "", `"crm"`)

	removed, err := DiffAppConfigVersions(to, from)
	if err != nil {
		t.Fatalf("diff array shrink: %v", err)
	}
	assertConfigDiffChange(t, removed, "/config_bundle_json/tools/2", AppConfigVersionDiffRemoved, `"crm"`, "")
}

func TestDiffAppConfigVersionsEscapesAmbiguousObjectKeys(t *testing.T) {
	from := validAppConfigVersion()
	from.ConfigBundleJSON = `{"a.b":1,"a":{"b":1},"slash/key":"old","tilde~key":"old","tools[1]":"old","tools":["search","ticket"]}`
	to := validAppConfigVersion()
	to.Version = "v2"
	to.Checksum = "sha256:2222"
	to.ConfigBundleJSON = `{"a.b":2,"a":{"b":3},"slash/key":"new","tilde~key":"new","tools[1]":"new","tools":["search","crm"]}`

	diff, err := DiffAppConfigVersions(from, to)
	if err != nil {
		t.Fatalf("diff escaped keys: %v", err)
	}
	assertConfigDiffChange(t, diff, "/config_bundle_json/a.b", AppConfigVersionDiffChanged, "1", "2")
	assertConfigDiffChange(t, diff, "/config_bundle_json/a/b", AppConfigVersionDiffChanged, "1", "3")
	assertConfigDiffChange(t, diff, "/config_bundle_json/slash~1key", AppConfigVersionDiffChanged, `"old"`, `"new"`)
	assertConfigDiffChange(t, diff, "/config_bundle_json/tilde~0key", AppConfigVersionDiffChanged, `"old"`, `"new"`)
	assertConfigDiffChange(t, diff, "/config_bundle_json/tools[1]", AppConfigVersionDiffChanged, `"old"`, `"new"`)
	assertConfigDiffChange(t, diff, "/config_bundle_json/tools/1", AppConfigVersionDiffChanged, `"ticket"`, `"crm"`)
}

func assertConfigDiffChange(
	t *testing.T,
	diff AppConfigVersionDiff,
	path string,
	kind AppConfigVersionDiffKind,
	before string,
	after string,
) {
	t.Helper()
	for _, change := range diff.Changes {
		if change.Path != path {
			continue
		}
		if change.Kind != kind || change.Before != before || change.After != after {
			t.Fatalf("unexpected change for %s: %+v", path, change)
		}
		return
	}
	t.Fatalf("missing change %s in %+v", path, diff.Changes)
}

func configDiffChangeValues(diff AppConfigVersionDiff) []string {
	values := make([]string, 0, len(diff.Changes)*2)
	for _, change := range diff.Changes {
		values = append(values, change.Before, change.After)
	}
	return values
}

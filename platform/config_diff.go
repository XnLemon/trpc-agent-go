//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

// AppConfigVersionDiffKind describes how one config version field changed.
type AppConfigVersionDiffKind string

const (
	// AppConfigVersionDiffAdded means the field exists only in the target version.
	AppConfigVersionDiffAdded AppConfigVersionDiffKind = "added"
	// AppConfigVersionDiffRemoved means the field exists only in the source version.
	AppConfigVersionDiffRemoved AppConfigVersionDiffKind = "removed"
	// AppConfigVersionDiffChanged means the field exists in both versions with different values.
	AppConfigVersionDiffChanged AppConfigVersionDiffKind = "changed"
)

const redactedConfigBundleDiffValue = `"***REDACTED***"`

// AppConfigVersionDiffChange is one safe, displayable config version difference.
type AppConfigVersionDiffChange struct {
	// Path is a metadata field name or a JSON Pointer-style config bundle path.
	Path   string
	Kind   AppConfigVersionDiffKind
	Before string
	After  string
}

// AppConfigVersionDiff summarizes differences between two versions owned by the same tenant app.
type AppConfigVersionDiff struct {
	TenantID    string
	AppID       string
	FromVersion string
	ToVersion   string
	Changes     []AppConfigVersionDiffChange
}

type missingConfigValue struct{}

// DiffAppConfigVersions compares safe metadata and config bundle values for two app config versions.
func DiffAppConfigVersions(from, to AppConfigVersion) (AppConfigVersionDiff, error) {
	if err := from.Validate(); err != nil {
		return AppConfigVersionDiff{}, fmt.Errorf("from config version: %w", err)
	}
	if err := to.Validate(); err != nil {
		return AppConfigVersionDiff{}, fmt.Errorf("to config version: %w", err)
	}
	if err := requireSameConfigOwner(from, to); err != nil {
		return AppConfigVersionDiff{}, err
	}
	diff := AppConfigVersionDiff{
		TenantID:    from.TenantID,
		AppID:       from.AppID,
		FromVersion: from.Version,
		ToVersion:   to.Version,
	}
	addConfigScalarChange(&diff.Changes, "version", from.Version, to.Version)
	addConfigScalarChange(&diff.Changes, "checksum", from.Checksum, to.Checksum)
	addConfigScalarChange(&diff.Changes, "status", string(from.Status), string(to.Status))
	addConfigScalarChange(&diff.Changes, "gray_percent", from.GrayPercent, to.GrayPercent)

	fromBundle, err := decodeConfigBundle(from.ConfigBundleJSON)
	if err != nil {
		return AppConfigVersionDiff{}, fmt.Errorf("from config bundle: %w", err)
	}
	toBundle, err := decodeConfigBundle(to.ConfigBundleJSON)
	if err != nil {
		return AppConfigVersionDiff{}, fmt.Errorf("to config bundle: %w", err)
	}
	diffConfigBundleValue("/config_bundle_json", fromBundle, toBundle, &diff.Changes)
	return diff, nil
}

func decodeConfigBundle(bundle string) (any, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(bundle))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("config bundle contains trailing json data")
		}
		return nil, err
	}
	return value, nil
}

func addConfigScalarChange(changes *[]AppConfigVersionDiffChange, path string, before, after any) {
	if reflect.DeepEqual(before, after) {
		return
	}
	*changes = append(*changes, AppConfigVersionDiffChange{
		Path:   path,
		Kind:   AppConfigVersionDiffChanged,
		Before: fmt.Sprint(before),
		After:  fmt.Sprint(after),
	})
}

func diffConfigBundleValue(path string, before, after any, changes *[]AppConfigVersionDiffChange) {
	if _, ok := before.(missingConfigValue); ok {
		*changes = append(*changes, AppConfigVersionDiffChange{
			Path:  path,
			Kind:  AppConfigVersionDiffAdded,
			After: formatConfigBundleDiffValue(path, after),
		})
		return
	}
	if _, ok := after.(missingConfigValue); ok {
		*changes = append(*changes, AppConfigVersionDiffChange{
			Path:   path,
			Kind:   AppConfigVersionDiffRemoved,
			Before: formatConfigBundleDiffValue(path, before),
		})
		return
	}

	beforeMap, beforeIsMap := before.(map[string]any)
	afterMap, afterIsMap := after.(map[string]any)
	if beforeIsMap && afterIsMap {
		for _, key := range sortedConfigKeys(beforeMap, afterMap) {
			childBefore, ok := beforeMap[key]
			if !ok {
				childBefore = missingConfigValue{}
			}
			childAfter, ok := afterMap[key]
			if !ok {
				childAfter = missingConfigValue{}
			}
			diffConfigBundleValue(path+"/"+escapeConfigPathSegment(key), childBefore, childAfter, changes)
		}
		return
	}

	beforeItems, beforeIsArray := before.([]any)
	afterItems, afterIsArray := after.([]any)
	if beforeIsArray && afterIsArray {
		maxLen := len(beforeItems)
		if len(afterItems) > maxLen {
			maxLen = len(afterItems)
		}
		for i := 0; i < maxLen; i++ {
			childBefore := any(missingConfigValue{})
			if i < len(beforeItems) {
				childBefore = beforeItems[i]
			}
			childAfter := any(missingConfigValue{})
			if i < len(afterItems) {
				childAfter = afterItems[i]
			}
			diffConfigBundleValue(fmt.Sprintf("%s/%d", path, i), childBefore, childAfter, changes)
		}
		return
	}

	if reflect.DeepEqual(before, after) {
		return
	}
	*changes = append(*changes, AppConfigVersionDiffChange{
		Path:   path,
		Kind:   AppConfigVersionDiffChanged,
		Before: formatConfigBundleDiffValue(path, before),
		After:  formatConfigBundleDiffValue(path, after),
	})
}

func escapeConfigPathSegment(segment string) string {
	segment = strings.ReplaceAll(segment, "~", "~0")
	return strings.ReplaceAll(segment, "/", "~1")
}

func sortedConfigKeys(left, right map[string]any) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
	}
	for key := range right {
		seen[key] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatConfigBundleValue(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

func formatConfigBundleDiffValue(path string, value any) string {
	if configBundleDiffPathSensitive(path) {
		return redactedConfigBundleDiffValue
	}
	return formatConfigBundleValue(redactConfigBundleDiffValue(path, value))
}

func redactConfigBundleDiffValue(path string, value any) any {
	if configBundleDiffPathSensitive(path) {
		return "***REDACTED***"
	}
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, child := range typed {
			redacted[key] = redactConfigBundleDiffValue(path+"/"+escapeConfigPathSegment(key), child)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for i, child := range typed {
			redacted[i] = redactConfigBundleDiffValue(fmt.Sprintf("%s/%d", path, i), child)
		}
		return redacted
	default:
		return value
	}
}

func configBundleDiffPathSensitive(path string) bool {
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		segment = strings.TrimSpace(unescapeConfigPathSegment(segment))
		if segment == "" {
			continue
		}
		if configBundleDiffSegmentSensitive(strings.ToLower(segment)) {
			return true
		}
	}
	return false
}

func configBundleDiffSegmentSensitive(segment string) bool {
	if strings.HasSuffix(segment, "_ref") {
		return true
	}
	switch segment {
	case "api_key", "apikey", "authorization", "cookie", "credential", "credentials",
		"password", "secret", "secrets", "token":
		return true
	}
	for _, suffix := range []string{
		"_api_key",
		"_apikey",
		"_authorization",
		"_cookie",
		"_credential",
		"_credentials",
		"_password",
		"_secret",
		"_token",
	} {
		if strings.HasSuffix(segment, suffix) {
			return true
		}
	}
	return false
}

func unescapeConfigPathSegment(segment string) string {
	segment = strings.ReplaceAll(segment, "~1", "/")
	return strings.ReplaceAll(segment, "~0", "~")
}

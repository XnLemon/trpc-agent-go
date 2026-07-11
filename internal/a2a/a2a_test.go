//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package a2a

import (
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestGetADKMetadataKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "app_name",
			key:      "app_name",
			expected: "adk_app_name",
		},
		{
			name:     "user_id",
			key:      "user_id",
			expected: "adk_user_id",
		},
		{
			name:     "type",
			key:      "type",
			expected: "adk_type",
		},
		{
			name:     "empty string",
			key:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetADKMetadataKey(tt.key)
			if result != tt.expected {
				t.Errorf("GetADKMetadataKey(%q) = %q, want %q", tt.key, result, tt.expected)
			}
		})
	}
}

func TestGetDataPartType(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
		expected string
	}{
		{
			name: "with adk_type",
			metadata: map[string]any{
				"adk_type": "function_call",
			},
			expected: "function_call",
		},
		{
			name: "with type",
			metadata: map[string]any{
				"type": "function_response",
			},
			expected: "function_response",
		},
		{
			name: "adk_type takes precedence",
			metadata: map[string]any{
				"adk_type": "executable_code",
				"type":     "function_call",
			},
			expected: "executable_code",
		},
		{
			name:     "nil metadata",
			metadata: nil,
			expected: "",
		},
		{
			name:     "empty metadata",
			metadata: map[string]any{},
			expected: "",
		},
		{
			name: "non-string type value",
			metadata: map[string]any{
				"type": 123,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetDataPartType(tt.metadata)
			if result != tt.expected {
				t.Errorf("GetDataPartType() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestResponseErrorMetadataHelpers(t *testing.T) {
	code := "A2A_42 token=raw-token"
	param := "Authorization: Bearer raw-token"
	metadata := WithResponseErrorMetadata(nil, &model.ResponseError{
		Type:    model.ErrorTypeFlowError,
		Message: sensitiveA2AErrorMessage(),
		Code:    &code,
		Param:   &param,
	})
	assertA2AErrorMessageRedacted(
		t,
		metadata[MessageMetadataErrorMessageKey].(string),
	)
	assertA2AErrorMessageRedacted(
		t,
		metadata[MessageMetadataErrorCodeKey].(string),
	)
	assertA2AErrorMessageRedacted(
		t,
		metadata[MessageMetadataErrorParamKey].(string),
	)

	got := ResponseErrorFromMetadata(
		metadata,
		"",
		model.ErrorTypeFlowError,
	)
	if got == nil {
		t.Fatal("ResponseErrorFromMetadata() returned nil")
	}
	if got.Type != model.ErrorTypeFlowError {
		t.Fatalf("Type = %q, want %q", got.Type, model.ErrorTypeFlowError)
	}
	assertA2AErrorMessageRedacted(t, got.Message)
	if got.Code == nil {
		t.Fatalf("Code = nil, want redacted value")
	}
	assertA2AErrorMessageRedacted(t, *got.Code)
	if got.Param == nil {
		t.Fatalf("Param = nil, want redacted value")
	}
	assertA2AErrorMessageRedacted(t, *got.Param)
}

func TestResponseErrorFromMetadata_IgnoresPlainTextFallback(t *testing.T) {
	got := ResponseErrorFromMetadata(
		nil,
		"plain response",
		model.ErrorTypeFlowError,
	)
	if got != nil {
		t.Fatalf("expected nil response error, got %+v", got)
	}
}

func TestResponseErrorFromMetadata_RedactsFallbackAndRawMetadata(t *testing.T) {
	code := "api_key=sk-testsecret"
	param := "Cookie: session=raw-cookie"
	got := ResponseErrorFromMetadata(
		map[string]any{
			MessageMetadataObjectTypeKey:   model.ObjectTypeError,
			MessageMetadataErrorTypeKey:    "flow_error secret: raw-secret",
			MessageMetadataErrorCodeKey:    code,
			MessageMetadataErrorParamKey:   param,
			MessageMetadataErrorMessageKey: "",
			MessageMetadataResponseIDKey:   "resp-1",
			MessageMetadataTaskStateKey:    "failed",
		},
		sensitiveA2AErrorMessage(),
		model.ErrorTypeFlowError,
	)
	if got == nil {
		t.Fatal("ResponseErrorFromMetadata() returned nil")
	}
	assertA2AErrorMessageRedacted(t, got.Type)
	assertA2AErrorMessageRedacted(t, got.Message)
	if got.Code == nil {
		t.Fatalf("Code = nil, want redacted value")
	}
	assertA2AErrorMessageRedacted(t, *got.Code)
	if got.Param == nil {
		t.Fatalf("Param = nil, want redacted value")
	}
	assertA2AErrorMessageRedacted(t, *got.Param)
}

func sensitiveA2AErrorMessage() string {
	return "task failed Authorization: Bearer raw-token api_key=sk-testsecret token=raw-token secret: raw-secret password=raw-password Cookie: session=raw-cookie"
}

func assertA2AErrorMessageRedacted(t *testing.T, message string) {
	t.Helper()
	for _, leaked := range []string{
		"raw-token",
		"sk-testsecret",
		"raw-secret",
		"raw-password",
		"raw-cookie",
	} {
		if strings.Contains(message, leaked) {
			t.Fatalf("redacted message leaked %q: %q", leaked, message)
		}
	}
	if !strings.Contains(message, "****") {
		t.Fatalf("redacted message missing mask: %q", message)
	}
}

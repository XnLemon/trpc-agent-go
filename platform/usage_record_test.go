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
	"math"
	"strings"
	"testing"
)

func TestUsageRecordValidateAcceptsSafeRecord(t *testing.T) {
	record := validUsageRecord()
	record.PromptTokens = 100
	record.CompletionTokens = 50
	record.CachedTokens = 10
	record.ModelUnitPrice = 0.00001
	record.ModelCost = 0.0015
	record.ToolCost = 0.25
	record.TotalCost = 0.2515

	if err := record.Validate(); err != nil {
		t.Fatalf("expected valid usage record, got %v", err)
	}
}

func TestUsageRecordValidateRequiresTenantAndApp(t *testing.T) {
	record := validUsageRecord()
	record.TenantID = " "
	if err := record.Validate(); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}

	record = validUsageRecord()
	record.AppID = " "
	if err := record.Validate(); !errors.Is(err, ErrAppIDRequired) {
		t.Fatalf("expected app requirement, got %v", err)
	}
}

func TestUsageRecordValidateRejectsNegativeTokens(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*UsageRecord)
	}{
		{name: "prompt", mutate: func(r *UsageRecord) { r.PromptTokens = -1 }},
		{name: "completion", mutate: func(r *UsageRecord) { r.CompletionTokens = -1 }},
		{name: "cached", mutate: func(r *UsageRecord) { r.CachedTokens = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validUsageRecord()
			tt.mutate(&record)
			if err := record.Validate(); err == nil || !strings.Contains(err.Error(), "token") {
				t.Fatalf("expected token validation, got %v", err)
			}
		})
	}
}

func TestUsageRecordValidateRejectsInvalidCosts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*UsageRecord)
		field  string
	}{
		{name: "model unit price negative", mutate: func(r *UsageRecord) { r.ModelUnitPrice = -0.01 }, field: "model_unit_price"},
		{name: "model unit price nan", mutate: func(r *UsageRecord) { r.ModelUnitPrice = math.NaN() }, field: "model_unit_price"},
		{name: "model cost negative", mutate: func(r *UsageRecord) { r.ModelCost = -0.01 }, field: "model_cost"},
		{name: "tool cost infinite", mutate: func(r *UsageRecord) { r.ToolCost = math.Inf(1) }, field: "tool_cost"},
		{name: "total cost negative", mutate: func(r *UsageRecord) { r.TotalCost = -0.01 }, field: "total_cost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validUsageRecord()
			tt.mutate(&record)
			if err := record.Validate(); err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("expected %s validation, got %v", tt.field, err)
			}
		})
	}
}

func TestUsageRecordValidateRejectsSensitiveDimensions(t *testing.T) {
	record := validUsageRecord()
	record.ToolName = "http_post Authorization: Bearer raw-token"
	if err := record.Validate(); err == nil || !strings.Contains(err.Error(), "tool_name") {
		t.Fatalf("expected sensitive tool name rejection, got %v", err)
	}
}

func validUsageRecord() UsageRecord {
	return UsageRecord{
		TenantID:   "tenant",
		AppID:      "app",
		UserIDHash: UserIDHash("tenant", "telegram", "external"),
		SessionID:  "session",
		RequestID:  "request",
		ModelName:  "gpt-test",
		ToolName:   "knowledge_search",
		TraceID:    "trace",
	}
}

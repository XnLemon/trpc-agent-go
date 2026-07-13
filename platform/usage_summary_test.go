//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestSummarizeUsageAggregatesTenantAndApp(t *testing.T) {
	records := []UsageRecord{
		usageRecordForSummary("tenant-a", "app-a", 100, 50, 10, 0.15, 0.20, 0.35),
		usageRecordForSummary("tenant-a", "app-a", 20, 5, 0, 0.02, 0.01, 0.03),
		usageRecordForSummary("tenant-a", "app-b", 1_000, 500, 0, 10, 1, 11),
		usageRecordForSummary("tenant-b", "app-a", 2_000, 600, 0, 20, 2, 22),
	}

	summary, err := SummarizeUsage(records, UsageSummaryFilter{TenantID: " tenant-a ", AppID: " app-a "})
	if err != nil {
		t.Fatalf("summarize usage: %v", err)
	}
	if summary.TenantID != "tenant-a" || summary.AppID != "app-a" {
		t.Fatalf("summary should expose normalized scope, got %+v", summary)
	}
	if summary.RecordCount != 2 {
		t.Fatalf("expected 2 records, got %d", summary.RecordCount)
	}
	if summary.PromptTokens != 120 ||
		summary.CompletionTokens != 55 ||
		summary.CachedTokens != 10 ||
		summary.TotalTokens != 175 {
		t.Fatalf("unexpected token totals: %+v", summary)
	}
	assertFloat(t, "ModelCost", summary.ModelCost, 0.17)
	assertFloat(t, "ToolCost", summary.ToolCost, 0.21)
	assertFloat(t, "TotalCost", summary.TotalCost, 0.38)
}

func TestSummarizeUsageAggregatesTenantAcrossApps(t *testing.T) {
	records := []UsageRecord{
		usageRecordForSummary("tenant-a", "app-a", 10, 20, 5, 0.1, 0.2, 0.3),
		usageRecordForSummary("tenant-a", "app-b", 30, 40, 0, 0.3, 0.4, 0.7),
		usageRecordForSummary("tenant-b", "app-a", 100, 100, 0, 1, 1, 2),
	}

	summary, err := SummarizeUsage(records, UsageSummaryFilter{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("summarize usage: %v", err)
	}
	if summary.RecordCount != 2 || summary.AppID != "" {
		t.Fatalf("expected tenant-wide summary, got %+v", summary)
	}
	if summary.TotalTokens != 100 {
		t.Fatalf("expected tenant token total 100, got %d", summary.TotalTokens)
	}
	assertFloat(t, "TotalCost", summary.TotalCost, 1.0)
}

func TestUsageSinkSummaryUsesSnapshot(t *testing.T) {
	sink := NewInMemoryUsageSink()
	if err := sink.WriteUsage(context.Background(), usageRecordForSummary("tenant", "app", 10, 5, 1, 0.1, 0.2, 0.3)); err != nil {
		t.Fatalf("write usage: %v", err)
	}
	if err := sink.WriteUsage(context.Background(), usageRecordForSummary("tenant", "app", 3, 2, 0, 0.01, 0.02, 0.03)); err != nil {
		t.Fatalf("write usage: %v", err)
	}

	summary, err := sink.Summary(UsageSummaryFilter{TenantID: "tenant", AppID: "app"})
	if err != nil {
		t.Fatalf("sink summary: %v", err)
	}
	if summary.RecordCount != 2 || summary.TotalTokens != 20 {
		t.Fatalf("unexpected sink summary: %+v", summary)
	}
	assertFloat(t, "TotalCost", summary.TotalCost, 0.33)
}

func TestSummarizeUsageRequiresTenant(t *testing.T) {
	_, err := SummarizeUsage(nil, UsageSummaryFilter{TenantID: " "})
	if !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}
}

func TestSummarizeUsageRejectsUnsafeFilterValues(t *testing.T) {
	tests := map[string]UsageSummaryFilter{
		"tenant_id": {
			TenantID: "tenant Authorization: Bearer raw-token",
		},
		"app_id": {
			TenantID: "tenant",
			AppID:    "app api_key=sk-1234567890abcdef",
		},
	}
	for name, filter := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := SummarizeUsage(nil, filter)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("expected unsafe %s filter rejection, got %v", name, err)
			}
		})
	}
}

func TestSummarizeUsageRejectsInvalidMatchingRecord(t *testing.T) {
	record := usageRecordForSummary("tenant", "app", 10, 5, 0, 0.1, 0.2, 0.3)
	record.TotalCost = math.Inf(1)

	_, err := SummarizeUsage([]UsageRecord{record}, UsageSummaryFilter{TenantID: "tenant"})
	if err == nil || !strings.Contains(err.Error(), "total_cost") {
		t.Fatalf("expected invalid matching record error, got %v", err)
	}
}

func TestSummarizeUsageIgnoresInvalidNonMatchingRecord(t *testing.T) {
	record := usageRecordForSummary("tenant-b", "app", 10, 5, 0, 0.1, 0.2, 0.3)
	record.TotalCost = math.Inf(1)

	summary, err := SummarizeUsage([]UsageRecord{record}, UsageSummaryFilter{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("non-matching record should not be validated, got %v", err)
	}
	if summary.RecordCount != 0 {
		t.Fatalf("expected empty summary, got %+v", summary)
	}
}

func TestSummarizeUsageRejectsTokenOverflow(t *testing.T) {
	records := []UsageRecord{
		usageRecordForSummary("tenant", "app", maxInt(), 0, 0, 0, 0, 0),
		usageRecordForSummary("tenant", "app", 1, 0, 0, 0, 0, 0),
	}

	_, err := SummarizeUsage(records, UsageSummaryFilter{TenantID: "tenant", AppID: "app"})
	if err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("expected token overflow error, got %v", err)
	}
}

func usageRecordForSummary(
	tenantID string,
	appID string,
	promptTokens int,
	completionTokens int,
	cachedTokens int,
	modelCost float64,
	toolCost float64,
	totalCost float64,
) UsageRecord {
	record := validUsageRecord()
	record.TenantID = tenantID
	record.AppID = appID
	record.PromptTokens = promptTokens
	record.CompletionTokens = completionTokens
	record.CachedTokens = cachedTokens
	record.ModelCost = modelCost
	record.ToolCost = toolCost
	record.TotalCost = totalCost
	return record
}

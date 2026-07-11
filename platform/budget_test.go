//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"math"
	"strings"
	"testing"
)

func TestParseTenantQuotaAllowsEmptyQuota(t *testing.T) {
	quota, err := ParseTenantQuota(Tenant{TenantID: "tenant"})
	if err != nil {
		t.Fatalf("parse empty quota: %v", err)
	}
	if quota != (TenantQuota{}) {
		t.Fatalf("empty quota should produce zero limits, got %+v", quota)
	}
}

func TestCheckTenantBudgetAllowsWithinQuota(t *testing.T) {
	tenant := Tenant{
		TenantID:  "tenant",
		QuotaJSON: `{"max_prompt_tokens":100,"max_completion_tokens":50,"max_total_tokens":150,"max_cost":1.25}`,
	}

	decision, err := CheckTenantBudget(tenant, UsageEstimate{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		Cost:             1.25,
	})

	if err != nil {
		t.Fatalf("check budget: %v", err)
	}
	if !decision.Allowed || decision.Reason != "" {
		t.Fatalf("expected allowed decision, got %+v", decision)
	}
}

func TestCheckUsageSummaryBudgetBuildsAllowedSnapshot(t *testing.T) {
	tenant := Tenant{
		TenantID:  "tenant",
		QuotaJSON: `{"max_prompt_tokens":100,"max_completion_tokens":50,"max_total_tokens":150,"max_cost":1.25}`,
	}
	summary := UsageSummary{
		TenantID:         "tenant",
		AppID:            "app",
		PromptTokens:     40,
		CompletionTokens: 20,
		TotalTokens:      60,
		TotalCost:        0.75,
	}

	snapshot, err := CheckUsageSummaryBudget(tenant, summary)
	if err != nil {
		t.Fatalf("CheckUsageSummaryBudget: %v", err)
	}

	if !snapshot.Decision.Allowed || snapshot.Decision.Reason != "" {
		t.Fatalf("expected allowed snapshot, got %+v", snapshot.Decision)
	}
	if snapshot.TenantID != "tenant" || snapshot.AppID != "app" {
		t.Fatalf("unexpected snapshot scope: %+v", snapshot)
	}
	if snapshot.PromptTokensRemaining != 60 ||
		snapshot.CompletionTokensRemaining != 30 ||
		snapshot.TotalTokensRemaining != 90 {
		t.Fatalf("unexpected token remaining values: %+v", snapshot)
	}
	assertFloat(t, "CostRemaining", snapshot.CostRemaining, 0.50)
}

func TestCheckUsageSummaryBudgetBuildsDeniedSnapshot(t *testing.T) {
	tenant := Tenant{
		TenantID:  "tenant",
		QuotaJSON: `{"max_total_tokens":100,"max_cost":1.00}`,
	}
	summary := UsageSummary{
		TenantID:         "tenant",
		PromptTokens:     80,
		CompletionTokens: 30,
		TotalTokens:      110,
		TotalCost:        0.50,
	}

	snapshot, err := CheckUsageSummaryBudget(tenant, summary)
	if err != nil {
		t.Fatalf("CheckUsageSummaryBudget: %v", err)
	}

	if snapshot.Decision.Allowed || snapshot.Decision.Reason != "total_tokens_exceeded" {
		t.Fatalf("expected total token denial, got %+v", snapshot.Decision)
	}
	if snapshot.TotalTokensRemaining != 0 {
		t.Fatalf("expected no remaining total tokens, got %+v", snapshot)
	}
}

func TestCheckUsageSummaryBudgetUsesEffectiveTotalTokens(t *testing.T) {
	tenant := Tenant{
		TenantID:  "tenant",
		QuotaJSON: `{"max_total_tokens":100}`,
	}
	summary := UsageSummary{
		TenantID:         "tenant",
		PromptTokens:     80,
		CompletionTokens: 30,
		TotalTokens:      1,
	}

	snapshot, err := CheckUsageSummaryBudget(tenant, summary)
	if err != nil {
		t.Fatalf("CheckUsageSummaryBudget: %v", err)
	}

	if snapshot.TotalTokensUsed != 110 || snapshot.TotalTokensRemaining != 0 {
		t.Fatalf("expected effective total token usage, got %+v", snapshot)
	}
	if snapshot.Decision.Allowed || snapshot.Decision.Reason != "total_tokens_exceeded" {
		t.Fatalf("expected effective total token denial, got %+v", snapshot.Decision)
	}
}

func TestBudgetUsageSnapshotAllowsUnlimitedQuotaRemaining(t *testing.T) {
	snapshot, err := CheckUsageSummaryBudget(
		Tenant{TenantID: "tenant"},
		UsageSummary{
			TenantID:         "tenant",
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			TotalCost:        0.25,
		},
	)
	if err != nil {
		t.Fatalf("CheckUsageSummaryBudget: %v", err)
	}

	if !snapshot.Decision.Allowed {
		t.Fatalf("expected unlimited quota to allow usage, got %+v", snapshot.Decision)
	}
	if snapshot.PromptTokensRemaining != 0 ||
		snapshot.CompletionTokensRemaining != 0 ||
		snapshot.TotalTokensRemaining != 0 ||
		snapshot.CostRemaining != 0 {
		t.Fatalf("expected unlimited remaining values to stay zero, got %+v", snapshot)
	}
}

func TestCheckUsageSummaryBudgetRejectsMismatchedTenant(t *testing.T) {
	_, err := CheckUsageSummaryBudget(
		Tenant{TenantID: "tenant-a"},
		UsageSummary{TenantID: "tenant-b"},
	)
	if err == nil || !strings.Contains(err.Error(), "tenant_id mismatch") {
		t.Fatalf("expected tenant mismatch error, got %v", err)
	}
}

func TestBudgetUsageSnapshotRejectsIncorrectRemaining(t *testing.T) {
	snapshot, err := CheckUsageSummaryBudget(
		Tenant{
			TenantID:  "tenant",
			QuotaJSON: `{"max_prompt_tokens":100,"max_completion_tokens":50,"max_total_tokens":150,"max_cost":1.00}`,
		},
		UsageSummary{
			TenantID:         "tenant",
			PromptTokens:     40,
			CompletionTokens: 20,
			TotalTokens:      60,
			TotalCost:        0.25,
		},
	)
	if err != nil {
		t.Fatalf("CheckUsageSummaryBudget: %v", err)
	}

	snapshot.TotalTokensRemaining = 123
	if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), "total_tokens_remaining") {
		t.Fatalf("expected remaining mismatch error, got %v", err)
	}

	snapshot, err = CheckUsageSummaryBudget(
		Tenant{TenantID: "tenant", QuotaJSON: `{"max_cost":1.00}`},
		UsageSummary{TenantID: "tenant", TotalCost: 0.25},
	)
	if err != nil {
		t.Fatalf("CheckUsageSummaryBudget cost: %v", err)
	}
	snapshot.CostRemaining = 0.10
	if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), "cost_remaining") {
		t.Fatalf("expected cost remaining mismatch error, got %v", err)
	}
}

func TestBudgetUsageSnapshotRejectsNonCanonicalTotalTokens(t *testing.T) {
	snapshot := BudgetUsageSnapshot{
		TenantID:             "tenant",
		PromptTokensUsed:     80,
		CompletionTokensUsed: 30,
		TotalTokensUsed:      1,
		MaxTotalTokens:       100,
		TotalTokensRemaining: 99,
		Decision:             BudgetDecision{Reason: "total_tokens_exceeded"},
	}

	if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), "effective total tokens") {
		t.Fatalf("expected canonical total token error, got %v", err)
	}
}

func TestCheckUsageSummaryBudgetRejectsInvalidSummaryValues(t *testing.T) {
	_, err := CheckUsageSummaryBudget(
		Tenant{TenantID: "tenant"},
		UsageSummary{TenantID: "tenant", PromptTokens: -1},
	)
	if err == nil || !strings.Contains(err.Error(), "usage estimate") {
		t.Fatalf("expected negative usage error, got %v", err)
	}

	_, err = CheckUsageSummaryBudget(
		Tenant{TenantID: "tenant"},
		UsageSummary{TenantID: "tenant", TotalCost: math.Inf(1)},
	)
	if err == nil || !strings.Contains(err.Error(), "cost") {
		t.Fatalf("expected non-finite cost error, got %v", err)
	}
}

func TestCheckUsageSummaryBudgetRejectsTokenOverflow(t *testing.T) {
	max := int(^uint(0) >> 1)
	_, err := CheckUsageSummaryBudget(
		Tenant{TenantID: "tenant"},
		UsageSummary{
			TenantID:         "tenant",
			PromptTokens:     max,
			CompletionTokens: 1,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("expected overflow error, got %v", err)
	}
}

func TestBudgetUsageSnapshotRejectsInconsistentDecision(t *testing.T) {
	_, err := NewBudgetUsageSnapshot(
		UsageSummary{TenantID: "tenant", TotalTokens: 200},
		TenantQuota{MaxTotalTokens: 100},
		BudgetDecision{Allowed: true},
	)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected decision mismatch error, got %v", err)
	}
}

func TestCheckTenantBudgetDeniesExceededLimits(t *testing.T) {
	tests := []struct {
		name     string
		quota    TenantQuota
		estimate UsageEstimate
		reason   string
	}{
		{
			name:     "prompt tokens",
			quota:    TenantQuota{MaxPromptTokens: 10},
			estimate: UsageEstimate{PromptTokens: 11},
			reason:   "prompt_tokens_exceeded",
		},
		{
			name:     "completion tokens",
			quota:    TenantQuota{MaxCompletionTokens: 10},
			estimate: UsageEstimate{CompletionTokens: 11},
			reason:   "completion_tokens_exceeded",
		},
		{
			name:     "total tokens",
			quota:    TenantQuota{MaxTotalTokens: 10},
			estimate: UsageEstimate{TotalTokens: 11},
			reason:   "total_tokens_exceeded",
		},
		{
			name:     "derived total tokens",
			quota:    TenantQuota{MaxTotalTokens: 10},
			estimate: UsageEstimate{PromptTokens: 6, CompletionTokens: 5},
			reason:   "total_tokens_exceeded",
		},
		{
			name:     "cost",
			quota:    TenantQuota{MaxCost: 1.25},
			estimate: UsageEstimate{Cost: 1.26},
			reason:   "cost_exceeded",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := tt.quota.Check(tt.estimate)
			if err != nil {
				t.Fatalf("check quota: %v", err)
			}
			if decision.Allowed || decision.Reason != tt.reason {
				t.Fatalf("expected denied %q, got %+v", tt.reason, decision)
			}
		})
	}
}

func TestTenantQuotaRejectsInvalidInputs(t *testing.T) {
	_, err := ParseTenantQuota(Tenant{
		TenantID:  "tenant",
		QuotaJSON: `{"max_total_tokens":`,
	})
	if err == nil {
		t.Fatalf("expected malformed quota json error")
	}
	_, err = ParseTenantQuota(Tenant{
		TenantID:  "tenant",
		QuotaJSON: `{"max_total_tokens":-1}`,
	})
	if err == nil {
		t.Fatalf("expected negative quota error")
	}
	_, err = TenantQuota{}.Check(UsageEstimate{Cost: -0.01})
	if err == nil {
		t.Fatalf("expected negative usage estimate error")
	}
}

func TestTenantQuotaRejectsNonFiniteCost(t *testing.T) {
	_, err := TenantQuota{MaxCost: math.NaN()}.Check(UsageEstimate{})
	if err == nil {
		t.Fatalf("expected non-finite quota cost error")
	}
	_, err = TenantQuota{}.Check(UsageEstimate{Cost: math.Inf(1)})
	if err == nil {
		t.Fatalf("expected non-finite usage cost error")
	}
}

func TestTenantQuotaRejectsTokenOverflow(t *testing.T) {
	max := int(^uint(0) >> 1)
	_, err := TenantQuota{MaxTotalTokens: max}.Check(UsageEstimate{
		PromptTokens:     max,
		CompletionTokens: 1,
	})
	if err == nil {
		t.Fatalf("expected total token overflow error")
	}
}

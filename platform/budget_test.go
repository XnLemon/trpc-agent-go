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

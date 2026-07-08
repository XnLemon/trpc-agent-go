//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// TenantQuota captures tenant-level budget limits from Tenant.QuotaJSON.
type TenantQuota struct {
	MaxPromptTokens     int     `json:"max_prompt_tokens,omitempty"`
	MaxCompletionTokens int     `json:"max_completion_tokens,omitempty"`
	MaxTotalTokens      int     `json:"max_total_tokens,omitempty"`
	MaxCost             float64 `json:"max_cost,omitempty"`
}

// UsageEstimate is the pre-run cost and token estimate checked against quota.
type UsageEstimate struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Cost             float64
}

// BudgetDecision describes whether a usage estimate is allowed by tenant quota.
type BudgetDecision struct {
	Allowed bool
	Reason  string
}

// ParseTenantQuota parses Tenant.QuotaJSON. Empty quota means no budget limits.
func ParseTenantQuota(tenant Tenant) (TenantQuota, error) {
	if err := tenant.Validate(); err != nil {
		return TenantQuota{}, err
	}
	quotaJSON := strings.TrimSpace(tenant.QuotaJSON)
	if quotaJSON == "" {
		return TenantQuota{}, nil
	}
	var quota TenantQuota
	if err := json.Unmarshal([]byte(quotaJSON), &quota); err != nil {
		return TenantQuota{}, fmt.Errorf("parsing tenant quota_json: %w", err)
	}
	if err := quota.Validate(); err != nil {
		return TenantQuota{}, err
	}
	return quota, nil
}

// CheckTenantBudget checks one usage estimate against the tenant's quota_json.
func CheckTenantBudget(tenant Tenant, estimate UsageEstimate) (BudgetDecision, error) {
	quota, err := ParseTenantQuota(tenant)
	if err != nil {
		return BudgetDecision{}, err
	}
	return quota.Check(estimate)
}

// Validate checks quota limits are non-negative.
func (q TenantQuota) Validate() error {
	if q.MaxPromptTokens < 0 {
		return fmt.Errorf("max_prompt_tokens must be non-negative")
	}
	if q.MaxCompletionTokens < 0 {
		return fmt.Errorf("max_completion_tokens must be non-negative")
	}
	if q.MaxTotalTokens < 0 {
		return fmt.Errorf("max_total_tokens must be non-negative")
	}
	if !isFiniteNonNegative(q.MaxCost) {
		return fmt.Errorf("max_cost must be finite and non-negative")
	}
	return nil
}

// Check applies quota limits to one usage estimate. Zero quota fields are unlimited.
func (q TenantQuota) Check(estimate UsageEstimate) (BudgetDecision, error) {
	if err := q.Validate(); err != nil {
		return BudgetDecision{}, err
	}
	if estimate.PromptTokens < 0 ||
		estimate.CompletionTokens < 0 ||
		estimate.TotalTokens < 0 {
		return BudgetDecision{}, fmt.Errorf("usage estimate values must be non-negative")
	}
	if !isFiniteNonNegative(estimate.Cost) {
		return BudgetDecision{}, fmt.Errorf("usage estimate cost must be finite and non-negative")
	}
	totalTokens, err := estimate.effectiveTotalTokens()
	if err != nil {
		return BudgetDecision{}, err
	}
	if q.MaxPromptTokens > 0 && estimate.PromptTokens > q.MaxPromptTokens {
		return BudgetDecision{Reason: "prompt_tokens_exceeded"}, nil
	}
	if q.MaxCompletionTokens > 0 && estimate.CompletionTokens > q.MaxCompletionTokens {
		return BudgetDecision{Reason: "completion_tokens_exceeded"}, nil
	}
	if q.MaxTotalTokens > 0 && totalTokens > q.MaxTotalTokens {
		return BudgetDecision{Reason: "total_tokens_exceeded"}, nil
	}
	if q.MaxCost > 0 && estimate.Cost > q.MaxCost {
		return BudgetDecision{Reason: "cost_exceeded"}, nil
	}
	return BudgetDecision{Allowed: true}, nil
}

func (e UsageEstimate) effectiveTotalTokens() (int, error) {
	total := e.TotalTokens
	if e.PromptTokens > maxInt()-e.CompletionTokens {
		return 0, fmt.Errorf("usage estimate total tokens overflow")
	}
	sum := e.PromptTokens + e.CompletionTokens
	if sum > total {
		return sum, nil
	}
	return total, nil
}

func isFiniteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

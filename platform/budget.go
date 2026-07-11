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

// BudgetUsageSnapshot captures accumulated usage against one quota boundary.
type BudgetUsageSnapshot struct {
	TenantID                  string
	AppID                     string
	PromptTokensUsed          int
	CompletionTokensUsed      int
	TotalTokensUsed           int
	CostUsed                  float64
	MaxPromptTokens           int
	MaxCompletionTokens       int
	MaxTotalTokens            int
	MaxCost                   float64
	PromptTokensRemaining     int
	CompletionTokensRemaining int
	TotalTokensRemaining      int
	CostRemaining             float64
	Decision                  BudgetDecision
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

// CheckUsageSummaryBudget checks accumulated usage against the tenant quota.
func CheckUsageSummaryBudget(tenant Tenant, summary UsageSummary) (BudgetUsageSnapshot, error) {
	quota, err := ParseTenantQuota(tenant)
	if err != nil {
		return BudgetUsageSnapshot{}, err
	}
	if strings.TrimSpace(summary.TenantID) != strings.TrimSpace(tenant.TenantID) {
		return BudgetUsageSnapshot{}, fmt.Errorf("usage summary tenant_id mismatch")
	}
	estimate := UsageEstimate{
		PromptTokens:     summary.PromptTokens,
		CompletionTokens: summary.CompletionTokens,
		TotalTokens:      summary.TotalTokens,
		Cost:             summary.TotalCost,
	}
	decision, err := quota.Check(estimate)
	if err != nil {
		return BudgetUsageSnapshot{}, err
	}
	return NewBudgetUsageSnapshot(summary, quota, decision)
}

// NewBudgetUsageSnapshot builds an accumulated usage snapshot for dashboards and budget counters.
func NewBudgetUsageSnapshot(
	summary UsageSummary,
	quota TenantQuota,
	decision BudgetDecision,
) (BudgetUsageSnapshot, error) {
	if err := quota.Validate(); err != nil {
		return BudgetUsageSnapshot{}, err
	}
	estimate := UsageEstimate{
		PromptTokens:     summary.PromptTokens,
		CompletionTokens: summary.CompletionTokens,
		TotalTokens:      summary.TotalTokens,
		Cost:             summary.TotalCost,
	}
	expected, err := quota.Check(estimate)
	if err != nil {
		return BudgetUsageSnapshot{}, err
	}
	effectiveTotalTokens, err := estimate.effectiveTotalTokens()
	if err != nil {
		return BudgetUsageSnapshot{}, err
	}
	if decision.Allowed != expected.Allowed || decision.Reason != expected.Reason {
		return BudgetUsageSnapshot{}, fmt.Errorf("budget decision does not match summary and quota")
	}
	snapshot := BudgetUsageSnapshot{
		TenantID:                  strings.TrimSpace(summary.TenantID),
		AppID:                     strings.TrimSpace(summary.AppID),
		PromptTokensUsed:          summary.PromptTokens,
		CompletionTokensUsed:      summary.CompletionTokens,
		TotalTokensUsed:           effectiveTotalTokens,
		CostUsed:                  summary.TotalCost,
		MaxPromptTokens:           quota.MaxPromptTokens,
		MaxCompletionTokens:       quota.MaxCompletionTokens,
		MaxTotalTokens:            quota.MaxTotalTokens,
		MaxCost:                   quota.MaxCost,
		PromptTokensRemaining:     remainingInt(quota.MaxPromptTokens, summary.PromptTokens),
		CompletionTokensRemaining: remainingInt(quota.MaxCompletionTokens, summary.CompletionTokens),
		TotalTokensRemaining:      remainingInt(quota.MaxTotalTokens, effectiveTotalTokens),
		CostRemaining:             remainingCost(quota.MaxCost, summary.TotalCost),
		Decision:                  decision,
	}
	if err := snapshot.Validate(); err != nil {
		return BudgetUsageSnapshot{}, err
	}
	return snapshot, nil
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

func remainingInt(limit int, used int) int {
	if limit <= 0 {
		return 0
	}
	if used >= limit {
		return 0
	}
	return limit - used
}

func remainingCost(limit float64, used float64) float64 {
	if limit <= 0 || used >= limit {
		return 0
	}
	return limit - used
}

// Validate checks that the budget snapshot is internally consistent.
func (s BudgetUsageSnapshot) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if err := validateAuditRedactedText("app_id", s.AppID); err != nil {
		return err
	}
	if s.PromptTokensUsed < 0 ||
		s.CompletionTokensUsed < 0 ||
		s.TotalTokensUsed < 0 {
		return fmt.Errorf("budget usage values must be non-negative")
	}
	if !isFiniteNonNegative(s.CostUsed) {
		return fmt.Errorf("budget cost used must be finite and non-negative")
	}
	if s.PromptTokensRemaining < 0 ||
		s.CompletionTokensRemaining < 0 ||
		s.TotalTokensRemaining < 0 {
		return fmt.Errorf("budget remaining token values must be non-negative")
	}
	if !isFiniteNonNegative(s.CostRemaining) {
		return fmt.Errorf("budget cost remaining must be finite and non-negative")
	}
	quota := TenantQuota{
		MaxPromptTokens:     s.MaxPromptTokens,
		MaxCompletionTokens: s.MaxCompletionTokens,
		MaxTotalTokens:      s.MaxTotalTokens,
		MaxCost:             s.MaxCost,
	}
	if err := quota.Validate(); err != nil {
		return err
	}
	estimate := UsageEstimate{
		PromptTokens:     s.PromptTokensUsed,
		CompletionTokens: s.CompletionTokensUsed,
		TotalTokens:      s.TotalTokensUsed,
		Cost:             s.CostUsed,
	}
	effectiveTotalTokens, err := estimate.effectiveTotalTokens()
	if err != nil {
		return err
	}
	if s.TotalTokensUsed != effectiveTotalTokens {
		return fmt.Errorf("total_tokens_used must match effective total tokens")
	}
	expected, err := quota.Check(estimate)
	if err != nil {
		return err
	}
	if s.Decision.Allowed != expected.Allowed || s.Decision.Reason != expected.Reason {
		return fmt.Errorf("budget snapshot decision does not match quota")
	}
	if s.PromptTokensRemaining != remainingInt(s.MaxPromptTokens, s.PromptTokensUsed) {
		return fmt.Errorf("prompt_tokens_remaining does not match quota")
	}
	if s.CompletionTokensRemaining != remainingInt(s.MaxCompletionTokens, s.CompletionTokensUsed) {
		return fmt.Errorf("completion_tokens_remaining does not match quota")
	}
	if s.TotalTokensRemaining != remainingInt(s.MaxTotalTokens, s.TotalTokensUsed) {
		return fmt.Errorf("total_tokens_remaining does not match quota")
	}
	if s.CostRemaining != remainingCost(s.MaxCost, s.CostUsed) {
		return fmt.Errorf("cost_remaining does not match quota")
	}
	return nil
}

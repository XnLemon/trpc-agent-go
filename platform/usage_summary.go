//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"fmt"
	"strings"
)

// UsageSummaryFilter scopes usage aggregation to one tenant and optionally one app.
type UsageSummaryFilter struct {
	TenantID string
	AppID    string
}

// UsageSummary aggregates post-run token and cost records for dashboards and budget checks.
type UsageSummary struct {
	TenantID         string
	AppID            string
	RecordCount      int
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	TotalTokens      int
	ModelCost        float64
	ToolCost         float64
	TotalCost        float64
}

// SummarizeUsage aggregates usage records for one tenant and optional app.
func SummarizeUsage(records []UsageRecord, filter UsageSummaryFilter) (UsageSummary, error) {
	tenantID := strings.TrimSpace(filter.TenantID)
	appID := strings.TrimSpace(filter.AppID)
	if tenantID == "" {
		return UsageSummary{}, ErrTenantIDRequired
	}
	summary := UsageSummary{
		TenantID: tenantID,
		AppID:    appID,
	}
	for _, record := range records {
		if strings.TrimSpace(record.TenantID) != tenantID {
			continue
		}
		if appID != "" && strings.TrimSpace(record.AppID) != appID {
			continue
		}
		if err := record.Validate(); err != nil {
			return UsageSummary{}, err
		}
		if err := summary.add(record); err != nil {
			return UsageSummary{}, err
		}
	}
	return summary, nil
}

// Summary returns an aggregate snapshot for the in-memory sink records.
func (s *InMemoryUsageSink) Summary(filter UsageSummaryFilter) (UsageSummary, error) {
	return SummarizeUsage(s.Records(), filter)
}

func (s *UsageSummary) add(record UsageRecord) error {
	var err error
	if s.RecordCount, err = addUsageInt("record_count", s.RecordCount, 1); err != nil {
		return err
	}
	if s.PromptTokens, err = addUsageInt("prompt_tokens", s.PromptTokens, record.PromptTokens); err != nil {
		return err
	}
	if s.CompletionTokens, err = addUsageInt("completion_tokens", s.CompletionTokens, record.CompletionTokens); err != nil {
		return err
	}
	if s.CachedTokens, err = addUsageInt("cached_tokens", s.CachedTokens, record.CachedTokens); err != nil {
		return err
	}
	if s.TotalTokens, err = addUsageInt("total_tokens", s.TotalTokens, record.PromptTokens); err != nil {
		return err
	}
	if s.TotalTokens, err = addUsageInt("total_tokens", s.TotalTokens, record.CompletionTokens); err != nil {
		return err
	}
	if s.ModelCost, err = addUsageCost("model_cost", s.ModelCost, record.ModelCost); err != nil {
		return err
	}
	if s.ToolCost, err = addUsageCost("tool_cost", s.ToolCost, record.ToolCost); err != nil {
		return err
	}
	if s.TotalCost, err = addUsageCost("total_cost", s.TotalCost, record.TotalCost); err != nil {
		return err
	}
	return nil
}

func addUsageInt(field string, current int, next int) (int, error) {
	if next < 0 {
		return 0, fmt.Errorf("%s must be non-negative", field)
	}
	if current > maxInt()-next {
		return 0, fmt.Errorf("%s overflow", field)
	}
	return current + next, nil
}

func addUsageCost(field string, current float64, next float64) (float64, error) {
	total := current + next
	if !isFiniteNonNegative(total) {
		return 0, fmt.Errorf("%s total must be finite and non-negative", field)
	}
	return total, nil
}

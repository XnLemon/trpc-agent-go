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
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ModelCostPolicy captures per-token model pricing from ModelProfile.CostPolicyJSON.
type ModelCostPolicy struct {
	InputTokenPricePerToken  float64 `json:"input_token_price_per_token,omitempty"`
	OutputTokenPricePerToken float64 `json:"output_token_price_per_token,omitempty"`
}

// ModelUsageCost is the calculated cost for one model usage payload.
type ModelUsageCost struct {
	UnitPrice float64
	Cost      float64
}

// ParseModelCostPolicy parses ModelProfile.CostPolicyJSON. Empty policy means zero-cost accounting.
func ParseModelCostPolicy(profile ModelProfile) (ModelCostPolicy, error) {
	costPolicyJSON := strings.TrimSpace(profile.CostPolicyJSON)
	if costPolicyJSON == "" {
		return ModelCostPolicy{}, nil
	}
	var policy ModelCostPolicy
	if err := json.Unmarshal([]byte(costPolicyJSON), &policy); err != nil {
		return ModelCostPolicy{}, fmt.Errorf("parsing model cost_policy_json: %w", err)
	}
	if err := policy.Validate(); err != nil {
		return ModelCostPolicy{}, err
	}
	return policy, nil
}

// Validate checks model pricing assumptions are safe to use for accounting.
func (p ModelCostPolicy) Validate() error {
	if !isFiniteNonNegative(p.InputTokenPricePerToken) {
		return fmt.Errorf("input_token_price_per_token must be finite and non-negative")
	}
	if !isFiniteNonNegative(p.OutputTokenPricePerToken) {
		return fmt.Errorf("output_token_price_per_token must be finite and non-negative")
	}
	return nil
}

// Cost calculates model cost for one usage payload.
func (p ModelCostPolicy) Cost(usage *model.Usage) (ModelUsageCost, error) {
	if err := p.Validate(); err != nil {
		return ModelUsageCost{}, err
	}
	if usage == nil {
		return ModelUsageCost{}, nil
	}
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 {
		return ModelUsageCost{}, fmt.Errorf("model usage token values must be non-negative")
	}
	cost := (float64(usage.PromptTokens) * p.InputTokenPricePerToken) +
		(float64(usage.CompletionTokens) * p.OutputTokenPricePerToken)
	totalTokens := usage.PromptTokens + usage.CompletionTokens
	unitPrice := 0.0
	if totalTokens > 0 {
		unitPrice = cost / float64(totalTokens)
	}
	return ModelUsageCost{
		UnitPrice: unitPrice,
		Cost:      cost,
	}, nil
}

// ModelUsageCostForProfile calculates model usage cost from a model profile.
func ModelUsageCostForProfile(profile ModelProfile, usage *model.Usage) (ModelUsageCost, error) {
	policy, err := ParseModelCostPolicy(profile)
	if err != nil {
		return ModelUsageCost{}, err
	}
	return policy.Cost(usage)
}

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

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestModelCostPolicyCalculatesUsageCost(t *testing.T) {
	policy, err := ParseModelCostPolicy(ModelProfile{
		TenantID:       "tenant",
		ProfileID:      "profile",
		CostPolicyJSON: `{"input_token_price_per_token":0.000001,"output_token_price_per_token":0.000002}`,
	})
	if err != nil {
		t.Fatalf("ParseModelCostPolicy: %v", err)
	}

	cost, err := policy.Cost(&model.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
	})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}

	assertFloat(t, "Cost", cost.Cost, 0.0002)
	assertFloat(t, "UnitPrice", cost.UnitPrice, 0.0002/150)
}

func TestParseModelCostPolicyAllowsEmptyPolicy(t *testing.T) {
	policy, err := ParseModelCostPolicy(ModelProfile{})
	if err != nil {
		t.Fatalf("ParseModelCostPolicy: %v", err)
	}

	cost, err := policy.Cost(&model.Usage{PromptTokens: 10, CompletionTokens: 5})
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}

	assertFloat(t, "Cost", cost.Cost, 0)
	assertFloat(t, "UnitPrice", cost.UnitPrice, 0)
}

func TestModelCostPolicyRejectsInvalidInputs(t *testing.T) {
	_, err := ParseModelCostPolicy(ModelProfile{CostPolicyJSON: `{"input_token_price_per_token":`})
	if err == nil || !strings.Contains(err.Error(), "cost_policy_json") {
		t.Fatalf("expected parse error, got %v", err)
	}

	_, err = ParseModelCostPolicy(ModelProfile{CostPolicyJSON: `{"input_token_price_per_token":-0.01}`})
	if err == nil || !strings.Contains(err.Error(), "input_token_price_per_token") {
		t.Fatalf("expected negative input price error, got %v", err)
	}

	_, err = ModelCostPolicy{OutputTokenPricePerToken: math.Inf(1)}.Cost(&model.Usage{})
	if err == nil || !strings.Contains(err.Error(), "output_token_price_per_token") {
		t.Fatalf("expected infinite output price error, got %v", err)
	}

	_, err = ModelCostPolicy{}.Cost(&model.Usage{PromptTokens: -1})
	if err == nil || !strings.Contains(err.Error(), "token values") {
		t.Fatalf("expected negative usage error, got %v", err)
	}
}

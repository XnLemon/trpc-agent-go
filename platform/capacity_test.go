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

func TestEstimateCapacityAppliesDesignFormulas(t *testing.T) {
	estimate, err := EstimateCapacity(CapacityInputs{
		DAU:                       1000,
		MessagesPerUserPeak:       3,
		PeakFactor:                4,
		PeakWindowSeconds:         600,
		AverageRunnerLatencySec:   8,
		TargetUtilization:         0.8,
		AverageEventsPerRun:       5,
		RequestsPerDay:            20000,
		AveragePromptTokens:       1000,
		AverageCompletionTokens:   250,
		InputTokenPricePerToken:   0.000001,
		OutputTokenPricePerToken:  0.000002,
		AverageToolCostPerRequest: 0.001,
	})
	if err != nil {
		t.Fatalf("EstimateCapacity: %v", err)
	}
	assertFloat(t, "CallbackQPS", estimate.CallbackQPS, 20)
	assertFloat(t, "WorkerConcurrency", estimate.WorkerConcurrency, 200)
	assertFloat(t, "SessionReadQPS", estimate.SessionReadQPS, 20)
	assertFloat(t, "SessionWriteQPS", estimate.SessionWriteQPS, 100)
	assertFloat(t, "TokensPerDay", estimate.TokensPerDay, 25_000_000)
	assertFloat(t, "CostPerDay", estimate.CostPerDay, 50)
}

func TestEstimateCapacityAllowsZeroDemand(t *testing.T) {
	estimate, err := EstimateCapacity(CapacityInputs{
		PeakWindowSeconds: 1,
		TargetUtilization: 1,
	})
	if err != nil {
		t.Fatalf("EstimateCapacity: %v", err)
	}
	if estimate != (CapacityEstimate{}) {
		t.Fatalf("expected zero estimate, got %+v", estimate)
	}
}

func TestCapacityInputsRejectInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		input CapacityInputs
		field string
	}{
		{
			name:  "negative dau",
			input: CapacityInputs{DAU: -1, PeakWindowSeconds: 1, TargetUtilization: 1},
			field: "dau",
		},
		{
			name:  "zero peak window",
			input: CapacityInputs{PeakWindowSeconds: 0, TargetUtilization: 1},
			field: "peak_window_seconds",
		},
		{
			name:  "zero utilization",
			input: CapacityInputs{PeakWindowSeconds: 1, TargetUtilization: 0},
			field: "target_utilization",
		},
		{
			name:  "utilization above one",
			input: CapacityInputs{PeakWindowSeconds: 1, TargetUtilization: 1.01},
			field: "target_utilization",
		},
		{
			name:  "nan peak factor",
			input: CapacityInputs{PeakWindowSeconds: 1, TargetUtilization: 1, PeakFactor: math.NaN()},
			field: "peak_factor",
		},
		{
			name:  "infinite latency",
			input: CapacityInputs{PeakWindowSeconds: 1, TargetUtilization: 1, AverageRunnerLatencySec: math.Inf(1)},
			field: "average_runner_latency_sec",
		},
		{
			name:  "negative requests",
			input: CapacityInputs{PeakWindowSeconds: 1, TargetUtilization: 1, RequestsPerDay: -1},
			field: "requests_per_day",
		},
		{
			name:  "negative token price",
			input: CapacityInputs{PeakWindowSeconds: 1, TargetUtilization: 1, InputTokenPricePerToken: -0.01},
			field: "input_token_price_per_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EstimateCapacity(tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("expected %s validation error, got %v", tt.field, err)
			}
		})
	}
}

func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

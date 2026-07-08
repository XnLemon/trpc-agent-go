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
	"math"
)

// CapacityInputs captures tenant-level planning assumptions.
type CapacityInputs struct {
	DAU                       int
	MessagesPerUserPeak       float64
	PeakFactor                float64
	PeakWindowSeconds         float64
	AverageRunnerLatencySec   float64
	TargetUtilization         float64
	AverageEventsPerRun       float64
	RequestsPerDay            int
	AveragePromptTokens       float64
	AverageCompletionTokens   float64
	InputTokenPricePerToken   float64
	OutputTokenPricePerToken  float64
	AverageToolCostPerRequest float64
}

// CapacityEstimate summarizes rough capacity and cost signals for one tenant.
type CapacityEstimate struct {
	CallbackQPS       float64
	WorkerConcurrency float64
	SessionReadQPS    float64
	SessionWriteQPS   float64
	TokensPerDay      float64
	CostPerDay        float64
}

// EstimateCapacity applies the platform capacity formulas to tenant inputs.
func EstimateCapacity(input CapacityInputs) (CapacityEstimate, error) {
	if err := input.Validate(); err != nil {
		return CapacityEstimate{}, err
	}
	callbackQPS := float64(input.DAU) * input.MessagesPerUserPeak * input.PeakFactor / input.PeakWindowSeconds
	workerConcurrency := callbackQPS * input.AverageRunnerLatencySec / input.TargetUtilization
	sessionWriteQPS := callbackQPS * input.AverageEventsPerRun
	requestsPerDay := float64(input.RequestsPerDay)
	tokensPerRequest := input.AveragePromptTokens + input.AverageCompletionTokens
	tokensPerDay := requestsPerDay * tokensPerRequest
	costPerDay := requestsPerDay * ((input.AveragePromptTokens * input.InputTokenPricePerToken) +
		(input.AverageCompletionTokens * input.OutputTokenPricePerToken) +
		input.AverageToolCostPerRequest)
	return CapacityEstimate{
		CallbackQPS:       callbackQPS,
		WorkerConcurrency: workerConcurrency,
		SessionReadQPS:    callbackQPS,
		SessionWriteQPS:   sessionWriteQPS,
		TokensPerDay:      tokensPerDay,
		CostPerDay:        costPerDay,
	}, nil
}

// Validate checks capacity assumptions before applying estimation formulas.
func (i CapacityInputs) Validate() error {
	if i.DAU < 0 {
		return fmt.Errorf("dau must be non-negative")
	}
	if i.RequestsPerDay < 0 {
		return fmt.Errorf("requests_per_day must be non-negative")
	}
	if !isFiniteNonNegative(i.MessagesPerUserPeak) {
		return fmt.Errorf("messages_per_user_peak must be finite and non-negative")
	}
	if !isFiniteNonNegative(i.PeakFactor) {
		return fmt.Errorf("peak_factor must be finite and non-negative")
	}
	if !isFinitePositive(i.PeakWindowSeconds) {
		return fmt.Errorf("peak_window_seconds must be finite and greater than 0")
	}
	if !isFiniteNonNegative(i.AverageRunnerLatencySec) {
		return fmt.Errorf("average_runner_latency_sec must be finite and non-negative")
	}
	if !isFinitePositive(i.TargetUtilization) || i.TargetUtilization > 1 {
		return fmt.Errorf("target_utilization must be finite and between 0 and 1")
	}
	if !isFiniteNonNegative(i.AverageEventsPerRun) {
		return fmt.Errorf("average_events_per_run must be finite and non-negative")
	}
	if !isFiniteNonNegative(i.AveragePromptTokens) {
		return fmt.Errorf("average_prompt_tokens must be finite and non-negative")
	}
	if !isFiniteNonNegative(i.AverageCompletionTokens) {
		return fmt.Errorf("average_completion_tokens must be finite and non-negative")
	}
	if !isFiniteNonNegative(i.InputTokenPricePerToken) {
		return fmt.Errorf("input_token_price_per_token must be finite and non-negative")
	}
	if !isFiniteNonNegative(i.OutputTokenPricePerToken) {
		return fmt.Errorf("output_token_price_per_token must be finite and non-negative")
	}
	if !isFiniteNonNegative(i.AverageToolCostPerRequest) {
		return fmt.Errorf("average_tool_cost_per_request must be finite and non-negative")
	}
	return nil
}

func isFinitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

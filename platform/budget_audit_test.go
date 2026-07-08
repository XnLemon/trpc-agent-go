//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewBudgetDecisionSummaryBuildsDenyAuditSummary(t *testing.T) {
	now := time.Unix(100, 0)
	quota := TenantQuota{MaxTotalTokens: 100, MaxCost: 1.25}
	estimate := UsageEstimate{PromptTokens: 80, CompletionTokens: 30, Cost: 0.50}
	decision, err := quota.Check(estimate)
	if err != nil {
		t.Fatalf("quota check: %v", err)
	}

	summary, err := NewBudgetDecisionSummary(BudgetDecisionAuditInput{
		TenantID:  " tenant ",
		AppID:     " app ",
		RequestID: " request-1 ",
		TraceID:   " trace-1 ",
		Decision:  decision,
		Estimate:  estimate,
		Quota:     quota,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewBudgetDecisionSummary: %v", err)
	}
	if summary.TenantID != "tenant" ||
		summary.AppID != "app" ||
		summary.RequestID != "request-1" ||
		summary.TraceID != "trace-1" ||
		summary.Outcome != BudgetDecisionOutcomeDeny ||
		summary.Reason != "total_tokens_exceeded" ||
		summary.EstimatedTotalTokens != 110 ||
		summary.MaxTotalTokens != 100 ||
		!summary.CreatedAt.Equal(now) {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	detail := summary.DetailRef()
	if !strings.Contains(detail, "outcome:deny") ||
		!strings.Contains(detail, "reason:total_tokens_exceeded") ||
		!strings.Contains(detail, "estimated_total_tokens:110") {
		t.Fatalf("detail ref missing decision fields: %q", detail)
	}
}

func TestNewBudgetDecisionAuditRecordBuildsStableRedactedRecord(t *testing.T) {
	now := time.Unix(200, 0)
	input := BudgetDecisionAuditInput{
		TenantID:  "tenant",
		AppID:     "app",
		RequestID: "request-1",
		TraceID:   "trace-1",
		Decision:  BudgetDecision{Reason: "cost_exceeded"},
		Estimate:  UsageEstimate{PromptTokens: 10, CompletionTokens: 5, Cost: 2.00},
		Quota:     TenantQuota{MaxCost: 1.00},
		CreatedAt: now,
	}
	record, err := NewBudgetDecisionAuditRecord(input)
	if err != nil {
		t.Fatalf("NewBudgetDecisionAuditRecord: %v", err)
	}
	if record.TenantID != "tenant" ||
		record.AppID != "app" ||
		record.ToolName != "budget:tenant" ||
		record.Decision != "deny" ||
		record.DecisionReason != "cost_exceeded" ||
		record.RequestID != "request-1" ||
		record.TraceID != "trace-1" ||
		record.RedactionVersion != "platform-budget-decision-v1" ||
		!record.CreatedAt.Equal(now) {
		t.Fatalf("unexpected audit record: %+v", record)
	}
	if !strings.Contains(record.TokenUsageJSON, "prompt_tokens:10") ||
		!strings.Contains(record.TokenUsageJSON, "completion_tokens:5") ||
		!strings.Contains(record.TokenUsageJSON, "total_tokens:15") {
		t.Fatalf("unexpected token usage ref: %q", record.TokenUsageJSON)
	}
	if strings.Contains(record.RedactedDetailRef, "sk-secret") ||
		strings.Contains(record.RedactedDetailRef, "password") {
		t.Fatalf("audit detail leaked secret content: %q", record.RedactedDetailRef)
	}

	again, err := NewBudgetDecisionAuditRecord(input)
	if err != nil {
		t.Fatalf("NewBudgetDecisionAuditRecord again: %v", err)
	}
	if record.AuditID != again.AuditID {
		t.Fatalf("expected stable audit id, got %q and %q", record.AuditID, again.AuditID)
	}
}

func TestNewBudgetDecisionSummaryBuildsDegradeOutcome(t *testing.T) {
	summary, err := NewBudgetDecisionSummary(BudgetDecisionAuditInput{
		TenantID:        "tenant",
		AppID:           "app",
		RequestID:       "request-1",
		TraceID:         "trace-1",
		Decision:        BudgetDecision{Reason: "cost_exceeded"},
		Estimate:        UsageEstimate{PromptTokens: 10, CompletionTokens: 5, Cost: 2.00},
		Quota:           TenantQuota{MaxCost: 1.00},
		DegradeStrategy: "fallback_model",
		CreatedAt:       time.Unix(300, 0),
	})
	if err != nil {
		t.Fatalf("NewBudgetDecisionSummary: %v", err)
	}
	if summary.Outcome != BudgetDecisionOutcomeDegrade ||
		summary.DegradeStrategy != "fallback_model" ||
		!strings.Contains(summary.DetailRef(), "degrade:fallback_model") {
		t.Fatalf("unexpected degrade summary: %+v", summary)
	}
}

func TestBudgetDecisionSummaryValidationRejectsUnsafeOrInconsistentFields(t *testing.T) {
	valid := BudgetDecisionSummary{
		TenantID:             "tenant",
		AppID:                "app",
		RequestID:            "request-1",
		TraceID:              "trace-1",
		Outcome:              BudgetDecisionOutcomeDeny,
		Reason:               "cost_exceeded",
		EstimatedPrompt:      10,
		EstimatedCompletion:  5,
		EstimatedTotalTokens: 15,
		EstimatedCost:        2.00,
		MaxCost:              1.00,
		RedactionVersion:     "platform-budget-decision-v1",
		CreatedAt:            time.Unix(400, 0),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate valid summary: %v", err)
	}

	unsafeTrace := valid
	unsafeTrace.TraceID = "token=sk-secret-token"
	if err := unsafeTrace.Validate(); err == nil || !strings.Contains(err.Error(), "trace_id") {
		t.Fatalf("expected unsafe trace rejection, got %v", err)
	}

	unsafeDetail := valid
	unsafeDetail.DegradeStrategy = "api_key: sk-secret-token"
	unsafeDetail.Outcome = BudgetDecisionOutcomeDegrade
	if err := unsafeDetail.Validate(); err == nil || !strings.Contains(err.Error(), "degrade_strategy") {
		t.Fatalf("expected unsafe degrade strategy rejection, got %v", err)
	}

	allowWithReason := valid
	allowWithReason.Outcome = BudgetDecisionOutcomeAllow
	allowWithReason.Reason = "should_be_empty"
	allowWithReason.EstimatedCost = 0.50
	allowWithReason.MaxCost = 1.00
	if err := allowWithReason.Validate(); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("expected allow reason rejection, got %v", err)
	}

	degradeWithoutStrategy := valid
	degradeWithoutStrategy.Outcome = BudgetDecisionOutcomeDegrade
	if err := degradeWithoutStrategy.Validate(); err == nil || !strings.Contains(err.Error(), "degrade_strategy") {
		t.Fatalf("expected missing degrade strategy rejection, got %v", err)
	}

	invalidEstimate := valid
	invalidEstimate.EstimatedCost = -0.01
	if err := invalidEstimate.Validate(); err == nil || !strings.Contains(err.Error(), "usage estimate cost") {
		t.Fatalf("expected invalid estimate rejection, got %v", err)
	}

	underReportedTotal := valid
	underReportedTotal.EstimatedPrompt = 80
	underReportedTotal.EstimatedCompletion = 30
	underReportedTotal.EstimatedTotalTokens = 1
	underReportedTotal.MaxTotalTokens = 100
	underReportedTotal.MaxCost = 0
	underReportedTotal.Reason = "total_tokens_exceeded"
	if err := underReportedTotal.Validate(); err == nil || !strings.Contains(err.Error(), "effective total tokens") {
		t.Fatalf("expected under-reported total rejection, got %v", err)
	}
}

func TestBudgetDecisionAuditInputRejectsMismatchedDecisionAndOutcome(t *testing.T) {
	_, err := NewBudgetDecisionSummary(BudgetDecisionAuditInput{
		TenantID:  "tenant",
		Decision:  BudgetDecision{Allowed: true},
		Outcome:   BudgetDecisionOutcomeDeny,
		Estimate:  UsageEstimate{},
		CreatedAt: time.Unix(500, 0),
	})
	if err == nil || !strings.Contains(err.Error(), "non-allow outcome") {
		t.Fatalf("expected mismatched denied outcome rejection, got %v", err)
	}

	_, err = NewBudgetDecisionSummary(BudgetDecisionAuditInput{
		TenantID:  "tenant",
		Decision:  BudgetDecision{Reason: "cost_exceeded"},
		Outcome:   BudgetDecisionOutcomeAllow,
		Estimate:  UsageEstimate{PromptTokens: 10, CompletionTokens: 5, Cost: 2.00},
		Quota:     TenantQuota{MaxCost: 1.00},
		CreatedAt: time.Unix(500, 0),
	})
	if err == nil || !strings.Contains(err.Error(), "allow outcome") {
		t.Fatalf("expected mismatched allow outcome rejection, got %v", err)
	}

	_, err = NewBudgetDecisionSummary(BudgetDecisionAuditInput{
		TenantID:  "tenant",
		Decision:  BudgetDecision{Reason: "cost_exceeded"},
		Estimate:  UsageEstimate{PromptTokens: 1, CompletionTokens: 1, Cost: 0.01},
		Quota:     TenantQuota{MaxCost: 1.00},
		CreatedAt: time.Unix(500, 0),
	})
	if err == nil || !strings.Contains(err.Error(), "budget decision") {
		t.Fatalf("expected decision/quota mismatch rejection, got %v", err)
	}

	_, err = NewBudgetDecisionSummary(BudgetDecisionAuditInput{
		TenantID:  "tenant",
		Decision:  BudgetDecision{Reason: "cost_exceeded"},
		Estimate:  UsageEstimate{PromptTokens: 10, CompletionTokens: 5, Cost: 2.00},
		Quota:     TenantQuota{MaxCost: 1.00},
		Outcome:   BudgetDecisionOutcomeDegrade,
		CreatedAt: time.Unix(500, 0),
	})
	if err == nil || !strings.Contains(err.Error(), "degrade_strategy") {
		t.Fatalf("expected explicit degrade strategy requirement, got %v", err)
	}
}

func TestNewBudgetDecisionSummaryRequiresTenant(t *testing.T) {
	_, err := NewBudgetDecisionSummary(BudgetDecisionAuditInput{
		Decision:  BudgetDecision{Allowed: true},
		CreatedAt: time.Unix(600, 0),
	})
	if !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}
}

func TestBudgetDecisionAuditIDIncludesEstimateAndQuotaBoundary(t *testing.T) {
	now := time.Unix(700, 0)
	base := BudgetDecisionAuditInput{
		TenantID:  "tenant",
		AppID:     "app",
		RequestID: "request-1",
		TraceID:   "trace-1",
		Decision:  BudgetDecision{Reason: "cost_exceeded"},
		Estimate:  UsageEstimate{PromptTokens: 10, CompletionTokens: 5, Cost: 2.00},
		Quota:     TenantQuota{MaxCost: 1.00},
		CreatedAt: now,
	}
	record, err := NewBudgetDecisionAuditRecord(base)
	if err != nil {
		t.Fatalf("NewBudgetDecisionAuditRecord base: %v", err)
	}

	differentEstimate := base
	differentEstimate.Estimate = UsageEstimate{PromptTokens: 10, CompletionTokens: 5, Cost: 3.00}
	differentEstimate.Quota = TenantQuota{MaxCost: 1.00}
	estimateRecord, err := NewBudgetDecisionAuditRecord(differentEstimate)
	if err != nil {
		t.Fatalf("NewBudgetDecisionAuditRecord different estimate: %v", err)
	}
	if record.AuditID == estimateRecord.AuditID {
		t.Fatalf("expected different audit id for changed estimate, got %q", record.AuditID)
	}

	differentQuota := base
	differentQuota.Quota = TenantQuota{MaxCost: 1.50}
	quotaRecord, err := NewBudgetDecisionAuditRecord(differentQuota)
	if err != nil {
		t.Fatalf("NewBudgetDecisionAuditRecord different quota: %v", err)
	}
	if record.AuditID == quotaRecord.AuditID {
		t.Fatalf("expected different audit id for changed quota, got %q", record.AuditID)
	}

	closeEstimate := base
	closeEstimate.Estimate = UsageEstimate{PromptTokens: 10, CompletionTokens: 5, Cost: 2.0000001}
	closeEstimateRecord, err := NewBudgetDecisionAuditRecord(closeEstimate)
	if err != nil {
		t.Fatalf("NewBudgetDecisionAuditRecord close estimate: %v", err)
	}
	if record.AuditID == closeEstimateRecord.AuditID {
		t.Fatalf("expected different audit id for close cost estimate, got %q", record.AuditID)
	}
}

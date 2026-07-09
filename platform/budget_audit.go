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
	"strconv"
	"strings"
	"time"
)

// BudgetDecisionOutcome is the externally visible budget gate outcome.
type BudgetDecisionOutcome string

const (
	// BudgetDecisionOutcomeAllow means the request can continue unchanged.
	BudgetDecisionOutcomeAllow BudgetDecisionOutcome = "allow"
	// BudgetDecisionOutcomeDeny means the request must be rejected.
	BudgetDecisionOutcomeDeny BudgetDecisionOutcome = "deny"
	// BudgetDecisionOutcomeDegrade means the request can continue with a lower-cost path.
	BudgetDecisionOutcomeDegrade BudgetDecisionOutcome = "degrade"
)

// BudgetDecisionAuditInput contains safe dimensions for a budget gate decision.
type BudgetDecisionAuditInput struct {
	TenantID        string
	AppID           string
	RequestID       string
	TraceID         string
	Decision        BudgetDecision
	Estimate        UsageEstimate
	Quota           TenantQuota
	Outcome         BudgetDecisionOutcome
	DegradeStrategy string
	CreatedAt       time.Time
}

// BudgetDecisionSummary is a safe, auditable summary of one budget decision.
type BudgetDecisionSummary struct {
	TenantID             string
	AppID                string
	RequestID            string
	TraceID              string
	Outcome              BudgetDecisionOutcome
	Reason               string
	DegradeStrategy      string
	EstimatedPrompt      int
	EstimatedCompletion  int
	EstimatedTotalTokens int
	EstimatedCost        float64
	MaxPromptTokens      int
	MaxCompletionTokens  int
	MaxTotalTokens       int
	MaxCost              float64
	RedactionVersion     string
	CreatedAt            time.Time
}

// NewBudgetDecisionSummary builds a safe summary for budget gate observability.
func NewBudgetDecisionSummary(input BudgetDecisionAuditInput) (BudgetDecisionSummary, error) {
	normalized, err := input.normalize()
	if err != nil {
		return BudgetDecisionSummary{}, err
	}
	totalTokens, err := normalized.Estimate.effectiveTotalTokens()
	if err != nil {
		return BudgetDecisionSummary{}, err
	}
	summary := BudgetDecisionSummary{
		TenantID:             normalized.TenantID,
		AppID:                normalized.AppID,
		RequestID:            normalized.RequestID,
		TraceID:              normalized.TraceID,
		Outcome:              normalized.Outcome,
		Reason:               strings.TrimSpace(normalized.Decision.Reason),
		DegradeStrategy:      normalized.DegradeStrategy,
		EstimatedPrompt:      normalized.Estimate.PromptTokens,
		EstimatedCompletion:  normalized.Estimate.CompletionTokens,
		EstimatedTotalTokens: totalTokens,
		EstimatedCost:        normalized.Estimate.Cost,
		MaxPromptTokens:      normalized.Quota.MaxPromptTokens,
		MaxCompletionTokens:  normalized.Quota.MaxCompletionTokens,
		MaxTotalTokens:       normalized.Quota.MaxTotalTokens,
		MaxCost:              normalized.Quota.MaxCost,
		RedactionVersion:     "platform-budget-decision-v1",
		CreatedAt:            normalized.CreatedAt,
	}
	if err := summary.Validate(); err != nil {
		return BudgetDecisionSummary{}, err
	}
	return summary, nil
}

// NewBudgetDecisionAuditRecord maps one budget decision into an audit record.
func NewBudgetDecisionAuditRecord(input BudgetDecisionAuditInput) (AuditRecord, error) {
	summary, err := NewBudgetDecisionSummary(input)
	if err != nil {
		return AuditRecord{}, err
	}
	record := AuditRecord{
		TenantID:          summary.TenantID,
		AppID:             summary.AppID,
		AuditID:           summary.auditID(),
		RequestID:         summary.RequestID,
		TraceID:           summary.TraceID,
		ToolName:          "budget:tenant",
		Decision:          string(summary.Outcome),
		DecisionReason:    summary.Reason,
		Cost:              summary.EstimatedCost,
		TokenUsageJSON:    summary.tokenUsageRef(),
		RedactedDetailRef: summary.DetailRef(),
		RedactionVersion:  summary.RedactionVersion,
		CreatedAt:         summary.CreatedAt,
	}
	if err := record.Validate(); err != nil {
		return AuditRecord{}, err
	}
	return record, nil
}

// Validate checks that the summary is complete and safe to expose.
func (s BudgetDecisionSummary) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if err := validateAuditRedactedText("app_id", s.AppID); err != nil {
		return err
	}
	if err := validateAuditRedactedText("request_id", s.RequestID); err != nil {
		return err
	}
	if err := validateAuditRedactedText("trace_id", s.TraceID); err != nil {
		return err
	}
	if err := validateAuditRedactedText("decision_reason", s.Reason); err != nil {
		return err
	}
	if err := validateAuditRedactedText("degrade_strategy", s.DegradeStrategy); err != nil {
		return err
	}
	if strings.TrimSpace(s.Reason) == "" && s.Outcome != BudgetDecisionOutcomeAllow {
		return fmt.Errorf("reason is required for non-allow budget outcomes")
	}
	estimate := UsageEstimate{
		PromptTokens:     s.EstimatedPrompt,
		CompletionTokens: s.EstimatedCompletion,
		TotalTokens:      s.EstimatedTotalTokens,
		Cost:             s.EstimatedCost,
	}
	if err := validateUsageEstimate(estimate); err != nil {
		return err
	}
	canonicalTotal, err := estimate.effectiveTotalTokens()
	if err != nil {
		return err
	}
	if canonicalTotal != s.EstimatedTotalTokens {
		return fmt.Errorf("estimated_total_tokens must match effective total tokens")
	}
	quota := s.quota()
	if err := quota.Validate(); err != nil {
		return err
	}
	expected, err := quota.Check(estimate)
	if err != nil {
		return err
	}
	switch s.Outcome {
	case BudgetDecisionOutcomeAllow:
		if !expected.Allowed {
			return fmt.Errorf("allow outcome does not match budget decision")
		}
		if strings.TrimSpace(s.Reason) != "" {
			return fmt.Errorf("reason must be empty for allow budget outcomes")
		}
		if strings.TrimSpace(s.DegradeStrategy) != "" {
			return fmt.Errorf("degrade_strategy must be empty for allow budget outcomes")
		}
	case BudgetDecisionOutcomeDeny:
		if expected.Allowed {
			return fmt.Errorf("deny outcome does not match budget decision")
		}
		if s.Reason != expected.Reason {
			return fmt.Errorf("reason must match budget decision")
		}
		if strings.TrimSpace(s.DegradeStrategy) != "" {
			return fmt.Errorf("degrade_strategy must be empty for deny budget outcomes")
		}
	case BudgetDecisionOutcomeDegrade:
		if expected.Allowed {
			return fmt.Errorf("degrade outcome does not match budget decision")
		}
		if s.Reason != expected.Reason {
			return fmt.Errorf("reason must match budget decision")
		}
		if strings.TrimSpace(s.DegradeStrategy) == "" {
			return fmt.Errorf("degrade_strategy is required for degrade budget outcomes")
		}
	case "":
		return fmt.Errorf("outcome is required")
	default:
		return fmt.Errorf("invalid budget decision outcome %q", s.Outcome)
	}
	if strings.TrimSpace(s.RedactionVersion) == "" {
		return fmt.Errorf("redaction_version is required")
	}
	if s.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	if err := validateAuditRedactedText("redacted_detail_ref", s.DetailRef()); err != nil {
		return err
	}
	return nil
}

// DetailRef returns compact non-secret detail for audit logs.
func (s BudgetDecisionSummary) DetailRef() string {
	parts := []string{
		"outcome:" + string(s.Outcome),
		"estimated_total_tokens:" + fmt.Sprint(s.EstimatedTotalTokens),
		"estimated_cost:" + fmt.Sprintf("%.6f", s.EstimatedCost),
	}
	if s.AppID != "" {
		parts = append(parts, "app:"+s.AppID)
	}
	if s.RequestID != "" {
		parts = append(parts, "request:"+s.RequestID)
	}
	if s.TraceID != "" {
		parts = append(parts, "trace:"+s.TraceID)
	}
	if s.Reason != "" {
		parts = append(parts, "reason:"+s.Reason)
	}
	if s.DegradeStrategy != "" {
		parts = append(parts, "degrade:"+s.DegradeStrategy)
	}
	if s.MaxPromptTokens > 0 {
		parts = append(parts, "max_prompt_tokens:"+fmt.Sprint(s.MaxPromptTokens))
	}
	if s.MaxCompletionTokens > 0 {
		parts = append(parts, "max_completion_tokens:"+fmt.Sprint(s.MaxCompletionTokens))
	}
	if s.MaxTotalTokens > 0 {
		parts = append(parts, "max_total_tokens:"+fmt.Sprint(s.MaxTotalTokens))
	}
	if s.MaxCost > 0 {
		parts = append(parts, "max_cost:"+fmt.Sprintf("%.6f", s.MaxCost))
	}
	return strings.Join(parts, " ")
}

func (i BudgetDecisionAuditInput) normalize() (BudgetDecisionAuditInput, error) {
	i.TenantID = strings.TrimSpace(i.TenantID)
	if i.TenantID == "" {
		return BudgetDecisionAuditInput{}, ErrTenantIDRequired
	}
	i.AppID = strings.TrimSpace(i.AppID)
	i.RequestID = strings.TrimSpace(i.RequestID)
	i.TraceID = strings.TrimSpace(i.TraceID)
	i.DegradeStrategy = strings.TrimSpace(i.DegradeStrategy)
	if err := validateUsageEstimate(i.Estimate); err != nil {
		return BudgetDecisionAuditInput{}, err
	}
	if err := i.Quota.Validate(); err != nil {
		return BudgetDecisionAuditInput{}, err
	}
	expected, err := i.Quota.Check(i.Estimate)
	if err != nil {
		return BudgetDecisionAuditInput{}, err
	}
	i.Decision.Reason = strings.TrimSpace(i.Decision.Reason)
	if i.Decision.Allowed != expected.Allowed {
		return BudgetDecisionAuditInput{}, fmt.Errorf("budget decision does not match quota and estimate")
	}
	if i.Decision.Reason != expected.Reason {
		return BudgetDecisionAuditInput{}, fmt.Errorf("budget decision reason does not match quota and estimate")
	}
	i.Outcome = normalizeBudgetOutcome(i.Decision, i.Outcome, i.DegradeStrategy)
	if i.Outcome == BudgetDecisionOutcomeAllow && !i.Decision.Allowed {
		return BudgetDecisionAuditInput{}, fmt.Errorf("allow outcome requires an allowed budget decision")
	}
	if i.Outcome != BudgetDecisionOutcomeAllow && i.Decision.Allowed {
		return BudgetDecisionAuditInput{}, fmt.Errorf("non-allow outcome requires a denied budget decision")
	}
	for field, value := range map[string]string{
		"app_id":           i.AppID,
		"request_id":       i.RequestID,
		"trace_id":         i.TraceID,
		"decision_reason":  i.Decision.Reason,
		"degrade_strategy": i.DegradeStrategy,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return BudgetDecisionAuditInput{}, err
		}
	}
	return i, nil
}

func normalizeBudgetOutcome(decision BudgetDecision, outcome BudgetDecisionOutcome, degradeStrategy string) BudgetDecisionOutcome {
	outcome = BudgetDecisionOutcome(strings.TrimSpace(string(outcome)))
	if outcome != "" {
		return outcome
	}
	if decision.Allowed {
		return BudgetDecisionOutcomeAllow
	}
	if strings.TrimSpace(degradeStrategy) != "" {
		return BudgetDecisionOutcomeDegrade
	}
	return BudgetDecisionOutcomeDeny
}

func (s BudgetDecisionSummary) auditID() string {
	return AuditID(
		s.TenantID,
		s.AppID,
		s.RequestID,
		s.TraceID,
		string(s.Outcome),
		s.Reason,
		s.DegradeStrategy,
		fmt.Sprint(s.EstimatedPrompt),
		fmt.Sprint(s.EstimatedCompletion),
		fmt.Sprint(s.EstimatedTotalTokens),
		canonicalBudgetCost(s.EstimatedCost),
		fmt.Sprint(s.MaxPromptTokens),
		fmt.Sprint(s.MaxCompletionTokens),
		fmt.Sprint(s.MaxTotalTokens),
		canonicalBudgetCost(s.MaxCost),
	)
}

func (s BudgetDecisionSummary) tokenUsageRef() string {
	return strings.Join([]string{
		"prompt_tokens:" + fmt.Sprint(s.EstimatedPrompt),
		"completion_tokens:" + fmt.Sprint(s.EstimatedCompletion),
		"total_tokens:" + fmt.Sprint(s.EstimatedTotalTokens),
	}, " ")
}

func (s BudgetDecisionSummary) quota() TenantQuota {
	return TenantQuota{
		MaxPromptTokens:     s.MaxPromptTokens,
		MaxCompletionTokens: s.MaxCompletionTokens,
		MaxTotalTokens:      s.MaxTotalTokens,
		MaxCost:             s.MaxCost,
	}
}

func validateUsageEstimate(estimate UsageEstimate) error {
	if estimate.PromptTokens < 0 ||
		estimate.CompletionTokens < 0 ||
		estimate.TotalTokens < 0 {
		return fmt.Errorf("usage estimate values must be non-negative")
	}
	if !isFiniteNonNegative(estimate.Cost) {
		return fmt.Errorf("usage estimate cost must be finite and non-negative")
	}
	if _, err := estimate.effectiveTotalTokens(); err != nil {
		return err
	}
	return nil
}

func canonicalBudgetCost(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

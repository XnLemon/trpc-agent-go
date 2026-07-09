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
	"math"
	"strings"
	"testing"
)

func TestAuditPolicyValidateAcceptsValidPolicy(t *testing.T) {
	policy := AuditPolicy{
		TenantID:              "tenant",
		PolicyID:              "audit-policy",
		RetentionDays:         30,
		SampleRate:            0.25,
		FullAuditForRiskyTool: true,
		RedactionRules:        []string{`(?i)(session_id=)[^\s]+`, "  "},
		ExportSink:            "audit://sink",
		ComplianceLevel:       "standard",
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("expected valid audit policy, got %v", err)
	}
}

func TestAuditPolicyValidateRequiresTenant(t *testing.T) {
	policy := validAuditPolicy()
	policy.TenantID = " "
	if err := policy.Validate(); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}
}

func TestAuditPolicyValidateRequiresPolicyID(t *testing.T) {
	policy := validAuditPolicy()
	policy.PolicyID = " "
	if err := policy.Validate(); err == nil || !strings.Contains(err.Error(), "policy_id is required") {
		t.Fatalf("expected policy_id requirement, got %v", err)
	}
}

func TestAuditPolicyValidateRejectsNegativeRetention(t *testing.T) {
	policy := validAuditPolicy()
	policy.RetentionDays = -1
	if err := policy.Validate(); err == nil || !strings.Contains(err.Error(), "retention_days") {
		t.Fatalf("expected retention_days validation, got %v", err)
	}
}

func TestAuditPolicyValidateRejectsInvalidSampleRate(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate float64
	}{
		{name: "negative", sampleRate: -0.01},
		{name: "above_one", sampleRate: 1.01},
		{name: "nan", sampleRate: math.NaN()},
		{name: "positive_inf", sampleRate: math.Inf(1)},
		{name: "negative_inf", sampleRate: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := validAuditPolicy()
			policy.SampleRate = tt.sampleRate
			if err := policy.Validate(); err == nil || !strings.Contains(err.Error(), "sample_rate") {
				t.Fatalf("expected sample_rate validation, got %v", err)
			}
		})
	}
}

func TestAuditPolicyValidateRejectsInvalidRedactionRule(t *testing.T) {
	policy := validAuditPolicy()
	policy.RedactionRules = []string{"["}
	if err := policy.Validate(); err == nil || !strings.Contains(err.Error(), "redaction_rules") {
		t.Fatalf("expected redaction_rules validation, got %v", err)
	}
}

func TestAuditPolicyValidateAcceptsBlankRedactionRule(t *testing.T) {
	policy := validAuditPolicy()
	policy.RedactionRules = []string{" "}
	if err := policy.Validate(); err != nil {
		t.Fatalf("expected blank redaction rule to be ignored, got %v", err)
	}
}

func TestAuditPolicyValidateAcceptsZeroSampleRate(t *testing.T) {
	policy := validAuditPolicy()
	policy.SampleRate = 0
	if err := policy.Validate(); err != nil {
		t.Fatalf("expected zero sample rate to be accepted, got %v", err)
	}
}

func validAuditPolicy() AuditPolicy {
	return AuditPolicy{
		TenantID:      "tenant",
		PolicyID:      "audit-policy",
		RetentionDays: 30,
		SampleRate:    1,
	}
}

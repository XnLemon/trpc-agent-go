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

func TestAuditRecordValidateAcceptsSafeRecord(t *testing.T) {
	record := validAuditRecord()
	record.DecisionReason = "tool approved by policy"
	record.TokenUsageJSON = `{"prompt_tokens":10,"completion_tokens":5}`
	record.RedactedDetailRef = "sha256:0123456789abcdef bytes:128"

	if err := record.Validate(); err != nil {
		t.Fatalf("expected valid audit record, got %v", err)
	}
}

func TestAuditRecordValidateRequiresTenant(t *testing.T) {
	record := validAuditRecord()
	record.TenantID = " "
	if err := record.Validate(); !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}
}

func TestAuditRecordValidateRequiresAuditID(t *testing.T) {
	record := validAuditRecord()
	record.AuditID = " "
	if err := record.Validate(); err == nil || !strings.Contains(err.Error(), "audit_id is required") {
		t.Fatalf("expected audit_id requirement, got %v", err)
	}
}

func TestAuditRecordValidateRejectsNegativeLatency(t *testing.T) {
	record := validAuditRecord()
	record.LatencyMS = -1
	if err := record.Validate(); err == nil || !strings.Contains(err.Error(), "latency_ms") {
		t.Fatalf("expected latency validation, got %v", err)
	}
}

func TestAuditRecordValidateRejectsInvalidCost(t *testing.T) {
	tests := []struct {
		name string
		cost float64
	}{
		{name: "negative", cost: -0.01},
		{name: "nan", cost: math.NaN()},
		{name: "positive_inf", cost: math.Inf(1)},
		{name: "negative_inf", cost: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validAuditRecord()
			record.Cost = tt.cost
			if err := record.Validate(); err == nil || !strings.Contains(err.Error(), "cost") {
				t.Fatalf("expected cost validation, got %v", err)
			}
		})
	}
}

func TestAuditRecordValidateRejectsSensitiveDecisionReason(t *testing.T) {
	record := validAuditRecord()
	record.DecisionReason = "Authorization: Bearer raw-token"
	if err := record.Validate(); err == nil || !strings.Contains(err.Error(), "decision_reason") {
		t.Fatalf("expected sensitive decision reason rejection, got %v", err)
	}
}

func TestAuditRecordValidateRejectsSensitiveRequestAndTraceIDs(t *testing.T) {
	tests := []struct {
		name  string
		mut   func(*AuditRecord)
		field string
	}{
		{
			name: "request_id",
			mut: func(record *AuditRecord) {
				record.RequestID = "Authorization: Bearer raw-token"
			},
			field: "request_id",
		},
		{
			name: "trace_id",
			mut: func(record *AuditRecord) {
				record.TraceID = "password=plain"
			},
			field: "trace_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validAuditRecord()
			tt.mut(&record)
			if err := record.Validate(); err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("expected sensitive %s rejection, got %v", tt.field, err)
			}
		})
	}
}

func TestAuditRecordValidateRejectsSensitiveErrorType(t *testing.T) {
	record := validAuditRecord()
	record.ErrorType = "storage_error password=plain"
	if err := record.Validate(); err == nil || !strings.Contains(err.Error(), "error_type") {
		t.Fatalf("expected sensitive error type rejection, got %v", err)
	}
}

func TestAuditRecordValidateRejectsSensitiveTokenUsage(t *testing.T) {
	record := validAuditRecord()
	record.TokenUsageJSON = `{"api_key":"sk-1234567890abcdef"}`
	if err := record.Validate(); err == nil || !strings.Contains(err.Error(), "token_usage_json") {
		t.Fatalf("expected sensitive token usage rejection, got %v", err)
	}
}

func TestAuditRecordValidateRejectsSensitiveDetailRef(t *testing.T) {
	record := validAuditRecord()
	record.RedactedDetailRef = "postgres://user:password@example.com/db"
	if err := record.Validate(); err == nil || !strings.Contains(err.Error(), "redacted_detail_ref") {
		t.Fatalf("expected sensitive detail rejection, got %v", err)
	}
}

func validAuditRecord() AuditRecord {
	return AuditRecord{
		TenantID:       "tenant",
		AuditID:        "audit",
		UserID:         "internal-user",
		InternalUserID: "usr",
		UserIDHash:     UserIDHash("tenant", "telegram", "external"),
		TraceID:        "trace",
		Decision:       "allow",
	}
}

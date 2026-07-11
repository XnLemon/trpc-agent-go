//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestQueryAuditFiltersTenantAndSafeDimensions(t *testing.T) {
	baseTime := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	userHash := UserIDHash("tenant-a", "telegram", "external-1")
	records := []AuditRecord{
		auditRecordForQuery("tenant-a", "audit-1", "app-a", "telegram", "binding-a", "session-1", "request-1", "message-1", "file_write", "deny", "trace-1", baseTime),
		auditRecordForQuery("tenant-a", "audit-2", "app-a", "telegram", "binding-a", "session-2", "request-2", "message-2", "file_write", "allow", "trace-2", baseTime.Add(time.Hour)),
		auditRecordForQuery("tenant-a", "audit-3", "app-b", "telegram", "binding-a", "session-1", "request-1", "message-1", "file_write", "deny", "trace-1", baseTime),
		auditRecordForQuery("tenant-b", "audit-4", "app-a", "telegram", "binding-a", "session-1", "request-1", "message-1", "file_write", "deny", "trace-1", baseTime),
	}
	records[0].UserIDHash = userHash
	records[1].UserIDHash = UserIDHash("tenant-a", "telegram", "external-2")

	matches, err := QueryAudit(records, AuditQueryFilter{
		TenantID:    " tenant-a ",
		AppID:       " app-a ",
		Channel:     "telegram",
		BindingID:   "binding-a",
		UserIDHash:  userHash,
		SessionID:   "session-1",
		RequestID:   "request-1",
		MessageID:   "message-1",
		ToolName:    "file_write",
		Decision:    "deny",
		TraceID:     "trace-1",
		CreatedFrom: baseTime.Add(-time.Minute),
		CreatedTo:   baseTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if len(matches) != 1 || matches[0].AuditID != "audit-1" {
		t.Fatalf("expected only audit-1, got %+v", matches)
	}
}

func TestQueryAuditSupportsAuditIDAndLimit(t *testing.T) {
	baseTime := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	records := []AuditRecord{
		auditRecordForQuery("tenant", "audit-1", "app", "telegram", "binding", "session", "request", "message", "tool", "allow", "trace", baseTime),
		auditRecordForQuery("tenant", "audit-2", "app", "telegram", "binding", "session", "request", "message", "tool", "allow", "trace", baseTime),
	}

	matches, err := QueryAudit(records, AuditQueryFilter{TenantID: "tenant", AuditID: "audit-2"})
	if err != nil {
		t.Fatalf("query by audit id: %v", err)
	}
	if len(matches) != 1 || matches[0].AuditID != "audit-2" {
		t.Fatalf("expected audit-2, got %+v", matches)
	}

	matches, err = QueryAudit(records, AuditQueryFilter{TenantID: "tenant", Decision: "allow", Limit: 1})
	if err != nil {
		t.Fatalf("query with limit: %v", err)
	}
	if len(matches) != 1 || matches[0].AuditID != "audit-1" {
		t.Fatalf("expected first limited result, got %+v", matches)
	}
}

func TestQueryAuditFiltersRuntimeAndRedactionDimensions(t *testing.T) {
	baseTime := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	target := auditRecordForQuery("tenant", "audit-1", "app", "wecom", "binding", "session", "request", "message", "workspace_write", "deny", "trace", baseTime)
	target.AgentName = "assistant"
	target.ModelName = "gpt-test"
	target.ErrorType = "permission_denied"
	target.RedactionVersion = "platform-toolpolicy-v1"
	otherAgent := target
	otherAgent.AuditID = "audit-2"
	otherAgent.AgentName = "other"
	otherModel := target
	otherModel.AuditID = "audit-3"
	otherModel.ModelName = "gpt-other"
	otherError := target
	otherError.AuditID = "audit-4"
	otherError.ErrorType = "runner_error"
	otherRedaction := target
	otherRedaction.AuditID = "audit-5"
	otherRedaction.RedactionVersion = "platform-budget-v1"

	matches, err := QueryAudit([]AuditRecord{
		otherAgent,
		otherModel,
		otherError,
		otherRedaction,
		target,
	}, AuditQueryFilter{
		TenantID:         "tenant",
		AgentName:        "assistant",
		ModelName:        "gpt-test",
		ErrorType:        "permission_denied",
		RedactionVersion: "platform-toolpolicy-v1",
	})
	if err != nil {
		t.Fatalf("query runtime dimensions: %v", err)
	}
	if len(matches) != 1 || matches[0].AuditID != "audit-1" {
		t.Fatalf("expected only audit-1, got %+v", matches)
	}
}

func TestQueryAuditFiltersBudgetDecisionByRedactedDetailRef(t *testing.T) {
	now := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	quota := TenantQuota{MaxCost: 1.00}
	estimate := UsageEstimate{PromptTokens: 10, CompletionTokens: 5, Cost: 2.00}
	decision, err := quota.Check(estimate)
	if err != nil {
		t.Fatalf("quota check: %v", err)
	}
	target, err := NewBudgetDecisionAuditRecord(BudgetDecisionAuditInput{
		TenantID:  "tenant",
		AppID:     "app",
		RequestID: "request-1",
		TraceID:   "trace-1",
		Decision:  decision,
		Estimate:  estimate,
		Quota:     quota,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewBudgetDecisionAuditRecord: %v", err)
	}
	other := target
	other.AuditID = "other-budget-audit"
	other.RedactedDetailRef = strings.ReplaceAll(target.RedactedDetailRef, "estimated_cost:2.000000", "estimated_cost:3.000000")

	matches, err := QueryAudit([]AuditRecord{other, target}, AuditQueryFilter{
		TenantID:          "tenant",
		ToolName:          "budget:tenant",
		Decision:          string(BudgetDecisionOutcomeDeny),
		RedactionVersion:  "platform-budget-decision-v1",
		RedactedDetailRef: " " + target.RedactedDetailRef + " ",
	})
	if err != nil {
		t.Fatalf("query budget audit detail: %v", err)
	}
	if len(matches) != 1 || matches[0].AuditID != target.AuditID {
		t.Fatalf("expected target budget audit, got %+v", matches)
	}
}

func TestAuditSinkQueryUsesSnapshotAndReturnsCopies(t *testing.T) {
	sink := NewInMemoryAuditSink()
	record := auditRecordForQuery("tenant", "audit-1", "app", "telegram", "binding", "session", "request", "message", "tool", "allow", "trace", time.Now())
	if err := sink.WriteAudit(context.Background(), record); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	matches, err := sink.Query(AuditQueryFilter{TenantID: "tenant", AppID: "app"})
	if err != nil {
		t.Fatalf("sink query: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one match, got %d", len(matches))
	}
	matches[0].TenantID = "changed"
	again, err := sink.Query(AuditQueryFilter{TenantID: "tenant", AppID: "app"})
	if err != nil {
		t.Fatalf("sink query again: %v", err)
	}
	if again[0].TenantID != "tenant" {
		t.Fatalf("query should return defensive copies, got %+v", again[0])
	}
}

func TestQueryAuditRequiresTenant(t *testing.T) {
	_, err := QueryAudit(nil, AuditQueryFilter{TenantID: " "})
	if !errors.Is(err, ErrTenantIDRequired) {
		t.Fatalf("expected tenant requirement, got %v", err)
	}
}

func TestQueryAuditRejectsUnsafeTenantFilter(t *testing.T) {
	_, err := QueryAudit(nil, AuditQueryFilter{TenantID: "tenant Authorization: Bearer raw-token"})
	if err == nil || !strings.Contains(err.Error(), "tenant_id") {
		t.Fatalf("expected unsafe tenant filter rejection, got %v", err)
	}
}

func TestQueryAuditRejectsUnsafeFilterValues(t *testing.T) {
	tests := map[string]AuditQueryFilter{
		"tool_name": {
			ToolName: "workspace_exec Authorization: Bearer raw-token",
		},
		"agent_name": {
			AgentName: "assistant Authorization: Bearer raw-token",
		},
		"model_name": {
			ModelName: "gpt-test password=plain",
		},
		"error_type": {
			ErrorType: "runner_error sk-1234567890abcdef",
		},
		"redaction_version": {
			RedactionVersion: "platform-v1 Authorization: Bearer raw-token",
		},
		"redacted_detail_ref": {
			RedactedDetailRef: "outcome:deny Authorization: Bearer raw-token",
		},
	}
	for name, filter := range tests {
		t.Run(name, func(t *testing.T) {
			filter.TenantID = "tenant"
			_, err := QueryAudit(nil, filter)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("expected unsafe %s filter error, got %v", name, err)
			}
		})
	}
}

func TestQueryAuditRejectsInvalidLimitAndTimeRange(t *testing.T) {
	_, err := QueryAudit(nil, AuditQueryFilter{TenantID: "tenant", Limit: -1})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected limit validation, got %v", err)
	}

	from := time.Date(2026, 7, 8, 11, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)
	_, err = QueryAudit(nil, AuditQueryFilter{TenantID: "tenant", CreatedFrom: from, CreatedTo: to})
	if err == nil || !strings.Contains(err.Error(), "created_from") {
		t.Fatalf("expected time range validation, got %v", err)
	}
}

func TestQueryAuditRejectsInvalidMatchingRecordOnly(t *testing.T) {
	matching := auditRecordForQuery("tenant-a", "audit-1", "app", "telegram", "binding", "session", "request", "message", "tool", "allow", "trace", time.Now())
	matching.LatencyMS = -1
	nonMatching := auditRecordForQuery("tenant-a", "audit-2", "other-app", "telegram", "binding", "session", "request", "message", "tool", "allow", "trace", time.Now())
	nonMatching.LatencyMS = -1
	otherTenant := auditRecordForQuery("tenant-b", "audit-2", "app", "telegram", "binding", "session", "request", "message", "tool", "allow", "trace", time.Now())
	otherTenant.LatencyMS = -1

	_, err := QueryAudit([]AuditRecord{otherTenant}, AuditQueryFilter{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("non-matching invalid record should not be validated, got %v", err)
	}
	_, err = QueryAudit([]AuditRecord{nonMatching}, AuditQueryFilter{TenantID: "tenant-a", AppID: "app"})
	if err != nil {
		t.Fatalf("same-tenant non-matching invalid record should not be validated, got %v", err)
	}

	_, err = QueryAudit([]AuditRecord{matching}, AuditQueryFilter{TenantID: "tenant-a"})
	if err == nil || !strings.Contains(err.Error(), "latency_ms") {
		t.Fatalf("expected invalid matching record error, got %v", err)
	}
}

func auditRecordForQuery(
	tenantID string,
	auditID string,
	appID string,
	channel string,
	bindingID string,
	sessionID string,
	requestID string,
	messageID string,
	toolName string,
	decision string,
	traceID string,
	createdAt time.Time,
) AuditRecord {
	record := validAuditRecord()
	record.TenantID = tenantID
	record.AuditID = auditID
	record.AppID = appID
	record.Channel = channel
	record.BindingID = bindingID
	record.SessionID = sessionID
	record.RequestID = requestID
	record.MessageID = messageID
	record.ToolName = toolName
	record.Decision = decision
	record.TraceID = traceID
	record.CreatedAt = createdAt
	return record
}

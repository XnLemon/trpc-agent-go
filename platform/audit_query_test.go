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

func TestQueryAuditRejectsUnsafeFilterValues(t *testing.T) {
	_, err := QueryAudit(nil, AuditQueryFilter{
		TenantID: "tenant",
		ToolName: "workspace_exec Authorization: Bearer raw-token",
	})
	if err == nil || !strings.Contains(err.Error(), "tool_name") {
		t.Fatalf("expected unsafe tool filter error, got %v", err)
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

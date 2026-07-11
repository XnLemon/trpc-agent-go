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
	"strings"
	"time"
)

// AuditQueryFilter scopes audit retrieval to one tenant and optional safe dimensions.
type AuditQueryFilter struct {
	TenantID          string
	AppID             string
	AuditID           string
	Channel           string
	BindingID         string
	UserIDHash        string
	SessionID         string
	RequestID         string
	MessageID         string
	AgentName         string
	ModelName         string
	ToolName          string
	Decision          string
	ErrorType         string
	TraceID           string
	RedactedDetailRef string
	RedactionVersion  string
	CreatedFrom       time.Time
	CreatedTo         time.Time
	Limit             int
}

// QueryAudit returns audit records matching one tenant-scoped filter.
func QueryAudit(records []AuditRecord, filter AuditQueryFilter) ([]AuditRecord, error) {
	normalized, err := filter.normalize()
	if err != nil {
		return nil, err
	}
	matches := make([]AuditRecord, 0)
	for _, record := range records {
		if !normalized.matchesScope(record) {
			continue
		}
		if normalized.matches(record) {
			if err := record.Validate(); err != nil {
				return nil, err
			}
			matches = append(matches, record)
			if normalized.Limit > 0 && len(matches) >= normalized.Limit {
				break
			}
		}
	}
	return matches, nil
}

// Query returns audit records matching one tenant-scoped filter.
func (s *InMemoryAuditSink) Query(filter AuditQueryFilter) ([]AuditRecord, error) {
	return QueryAudit(s.Records(), filter)
}

func (f AuditQueryFilter) normalize() (AuditQueryFilter, error) {
	f.TenantID = strings.TrimSpace(f.TenantID)
	if f.TenantID == "" {
		return AuditQueryFilter{}, ErrTenantIDRequired
	}
	if err := validateAuditRedactedFields(
		safeTextField{"app_id", f.AppID},
		safeTextField{"audit_id", f.AuditID},
		safeTextField{"channel", f.Channel},
		safeTextField{"binding_id", f.BindingID},
		safeTextField{"user_id_hash", f.UserIDHash},
		safeTextField{"session_id", f.SessionID},
		safeTextField{"request_id", f.RequestID},
		safeTextField{"message_id", f.MessageID},
		safeTextField{"agent_name", f.AgentName},
		safeTextField{"model_name", f.ModelName},
		safeTextField{"tool_name", f.ToolName},
		safeTextField{"decision", f.Decision},
		safeTextField{"error_type", f.ErrorType},
		safeTextField{"trace_id", f.TraceID},
		safeTextField{"redacted_detail_ref", f.RedactedDetailRef},
		safeTextField{"redaction_version", f.RedactionVersion},
	); err != nil {
		return AuditQueryFilter{}, err
	}
	if f.Limit < 0 {
		return AuditQueryFilter{}, fmt.Errorf("limit must be non-negative")
	}
	if !f.CreatedFrom.IsZero() && !f.CreatedTo.IsZero() && f.CreatedFrom.After(f.CreatedTo) {
		return AuditQueryFilter{}, fmt.Errorf("created_from must be before or equal to created_to")
	}
	f.AppID = strings.TrimSpace(f.AppID)
	f.AuditID = strings.TrimSpace(f.AuditID)
	f.Channel = strings.TrimSpace(f.Channel)
	f.BindingID = strings.TrimSpace(f.BindingID)
	f.UserIDHash = strings.TrimSpace(f.UserIDHash)
	f.SessionID = strings.TrimSpace(f.SessionID)
	f.RequestID = strings.TrimSpace(f.RequestID)
	f.MessageID = strings.TrimSpace(f.MessageID)
	f.AgentName = strings.TrimSpace(f.AgentName)
	f.ModelName = strings.TrimSpace(f.ModelName)
	f.ToolName = strings.TrimSpace(f.ToolName)
	f.Decision = strings.TrimSpace(f.Decision)
	f.ErrorType = strings.TrimSpace(f.ErrorType)
	f.TraceID = strings.TrimSpace(f.TraceID)
	f.RedactedDetailRef = strings.TrimSpace(f.RedactedDetailRef)
	f.RedactionVersion = strings.TrimSpace(f.RedactionVersion)
	return f, nil
}

func (f AuditQueryFilter) matchesScope(record AuditRecord) bool {
	return strings.TrimSpace(record.TenantID) == f.TenantID
}

func (f AuditQueryFilter) matches(record AuditRecord) bool {
	return f.matchesOptionalFields(record) && f.matchesCreatedAt(record.CreatedAt)
}

func (f AuditQueryFilter) matchesOptionalFields(record AuditRecord) bool {
	for _, field := range []struct {
		want string
		got  string
	}{
		{f.AppID, record.AppID},
		{f.AuditID, record.AuditID},
		{f.Channel, record.Channel},
		{f.BindingID, record.BindingID},
		{f.UserIDHash, record.UserIDHash},
		{f.SessionID, record.SessionID},
		{f.RequestID, record.RequestID},
		{f.MessageID, record.MessageID},
		{f.AgentName, record.AgentName},
		{f.ModelName, record.ModelName},
		{f.ToolName, record.ToolName},
		{f.Decision, record.Decision},
		{f.ErrorType, record.ErrorType},
		{f.RedactedDetailRef, record.RedactedDetailRef},
		{f.RedactionVersion, record.RedactionVersion},
		{f.TraceID, record.TraceID},
	} {
		if !matchOptional(field.want, field.got) {
			return false
		}
	}
	return true
}

func (f AuditQueryFilter) matchesCreatedAt(createdAt time.Time) bool {
	if !f.CreatedFrom.IsZero() && createdAt.Before(f.CreatedFrom) {
		return false
	}
	if !f.CreatedTo.IsZero() && createdAt.After(f.CreatedTo) {
		return false
	}
	return true
}

func matchOptional(want, got string) bool {
	return want == "" || strings.TrimSpace(got) == want
}

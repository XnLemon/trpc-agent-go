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
	TenantID    string
	AppID       string
	AuditID     string
	Channel     string
	BindingID   string
	UserIDHash  string
	SessionID   string
	RequestID   string
	MessageID   string
	ToolName    string
	Decision    string
	TraceID     string
	CreatedFrom time.Time
	CreatedTo   time.Time
	Limit       int
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
	for field, value := range map[string]string{
		"app_id":       f.AppID,
		"audit_id":     f.AuditID,
		"channel":      f.Channel,
		"binding_id":   f.BindingID,
		"user_id_hash": f.UserIDHash,
		"session_id":   f.SessionID,
		"request_id":   f.RequestID,
		"message_id":   f.MessageID,
		"tool_name":    f.ToolName,
		"decision":     f.Decision,
		"trace_id":     f.TraceID,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return AuditQueryFilter{}, err
		}
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
	f.ToolName = strings.TrimSpace(f.ToolName)
	f.Decision = strings.TrimSpace(f.Decision)
	f.TraceID = strings.TrimSpace(f.TraceID)
	return f, nil
}

func (f AuditQueryFilter) matchesScope(record AuditRecord) bool {
	return strings.TrimSpace(record.TenantID) == f.TenantID
}

func (f AuditQueryFilter) matches(record AuditRecord) bool {
	if !matchOptional(f.AppID, record.AppID) ||
		!matchOptional(f.AuditID, record.AuditID) ||
		!matchOptional(f.Channel, record.Channel) ||
		!matchOptional(f.BindingID, record.BindingID) ||
		!matchOptional(f.UserIDHash, record.UserIDHash) ||
		!matchOptional(f.SessionID, record.SessionID) ||
		!matchOptional(f.RequestID, record.RequestID) ||
		!matchOptional(f.MessageID, record.MessageID) ||
		!matchOptional(f.ToolName, record.ToolName) ||
		!matchOptional(f.Decision, record.Decision) ||
		!matchOptional(f.TraceID, record.TraceID) {
		return false
	}
	if !f.CreatedFrom.IsZero() && record.CreatedAt.Before(f.CreatedFrom) {
		return false
	}
	if !f.CreatedTo.IsZero() && record.CreatedAt.After(f.CreatedTo) {
		return false
	}
	return true
}

func matchOptional(want, got string) bool {
	return want == "" || strings.TrimSpace(got) == want
}

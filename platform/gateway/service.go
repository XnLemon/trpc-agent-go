//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/platform/channeladapter"
)

// Service handles normalized inbound platform messages.
type Service struct {
	registry         Registry
	idempotencyStore platform.IdempotencyStore
	outboundStore    OutboundStore
	auditSink        platform.AuditSink
	now              func() time.Time
}

// Option configures a Service.
type Option func(*Service)

// WithAuditSink sets the audit sink used by the service.
func WithAuditSink(sink platform.AuditSink) Option {
	return func(s *Service) {
		s.auditSink = sink
	}
}

// WithNow sets the clock used by the service.
func WithNow(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// NewService creates a gateway service.
func NewService(
	registry Registry,
	idempotencyStore platform.IdempotencyStore,
	outboundStore OutboundStore,
	opts ...Option,
) *Service {
	svc := &Service{
		registry:         registry,
		idempotencyStore: idempotencyStore,
		outboundStore:    outboundStore,
		now:              time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc
}

// Result describes the outcome of handling an inbound platform message.
type Result struct {
	RequestID   string
	SessionID   string
	ResultRef   string
	Status      platform.IdempotencyStatus
	Outbound    platform.OutboundMessage
	Duplicate   bool
	Processing  bool
	CompletedAt time.Time
}

// HandleInbound validates, deduplicates, runs, and records a text-only inbound message.
func (s *Service) HandleInbound(
	ctx context.Context,
	msg platform.InboundMessage,
) (Result, error) {
	start := s.now()
	if err := s.validateService(); err != nil {
		return Result{}, err
	}
	if err := msg.Validate(); err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		return Result{}, err
	}
	runtime, ok, err := s.registry.Lookup(ctx, msg)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		err := ErrRuntimeNotFound
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		return Result{}, err
	}
	if err := runtime.Validate(); err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		return Result{}, err
	}
	if !runtime.matchesInbound(msg) {
		err := ErrRuntimeMismatch
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		return Result{}, err
	}
	if err := authorizeBinding(runtime.Binding, msg); err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		return Result{}, err
	}
	text, err := inboundText(msg)
	if err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		return Result{}, err
	}
	sessionID, err := platform.SessionIDForInbound(msg)
	if err != nil {
		return Result{}, err
	}
	internalUserID := platform.InternalUserID(msg.TenantID, msg.Channel, msg.ExternalUserID)
	requestID := requestIDFor(msg)
	key := platform.IdempotencyKey(
		msg.TenantID,
		msg.Channel,
		msg.ChannelAccountID,
		msg.PlatformMessageID,
	)
	record, started, err := s.idempotencyStore.Start(ctx, platform.IdempotencyRecord{
		TenantID:          msg.TenantID,
		Channel:           msg.Channel,
		AccountID:         msg.ChannelAccountID,
		PlatformMessageID: msg.PlatformMessageID,
		IdempotencyKey:    key,
		RequestID:         requestID,
		SessionID:         sessionID,
	})
	if err != nil {
		return Result{}, err
	}
	if !started {
		return s.duplicateResult(ctx, record)
	}

	ch, err := runtime.Runner.Run(
		ctx,
		internalUserID,
		sessionID,
		model.NewUserMessage(text),
		agent.WithRequestID(requestID),
	)
	if err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, sessionID, internalUserID, "runner_error", err.Error(), start, err))
		return Result{}, err
	}
	content, err := collectAssistantText(ch)
	if err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, sessionID, internalUserID, "runner_error", err.Error(), start, err))
		return Result{}, err
	}
	resultRef := key + ":outbound:1"
	outbound := platform.OutboundMessage{
		TenantID:                 msg.TenantID,
		BindingID:                msg.BindingID,
		Channel:                  msg.Channel,
		SessionID:                sessionID,
		ReplyToPlatformMessageID: msg.PlatformMessageID,
		Kind:                     platform.OutboundMessageKindText,
		Content:                  content,
		Sequence:                 1,
		DedupKey:                 resultRef,
		TraceID:                  requestID,
	}
	if err := s.outboundStore.Save(ctx, resultRef, outbound); err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, sessionID, internalUserID, "outbound_error", err.Error(), start, err))
		return Result{}, err
	}
	if err := s.outboundStore.Enqueue(
		ctx,
		outbound,
		channeladapter.RetryPolicyForBinding(runtime.Binding),
	); err != nil {
		if _, markErr := s.idempotencyStore.MarkReplyFailed(ctx, key, resultRef); markErr != nil {
			return Result{}, markErr
		}
		s.writeAudit(ctx, auditFromMessage(msg, sessionID, internalUserID, "outbound_error", err.Error(), start, err))
		return Result{}, err
	}
	record, err = s.idempotencyStore.Complete(ctx, key, resultRef)
	if err != nil {
		return Result{}, err
	}
	s.writeAudit(ctx, auditFromMessage(msg, sessionID, internalUserID, "completed", "", start, nil))
	return Result{
		RequestID:   requestID,
		SessionID:   sessionID,
		ResultRef:   resultRef,
		Status:      record.Status,
		Outbound:    outbound,
		CompletedAt: s.now(),
	}, nil
}

func (s *Service) validateService() error {
	if s.registry == nil {
		return fmt.Errorf("gateway registry is required")
	}
	if s.idempotencyStore == nil {
		return fmt.Errorf("gateway idempotency store is required")
	}
	if s.outboundStore == nil {
		return fmt.Errorf("gateway outbound store is required")
	}
	return nil
}

func (s *Service) duplicateResult(
	ctx context.Context,
	record platform.IdempotencyRecord,
) (Result, error) {
	result := Result{
		RequestID:  record.RequestID,
		SessionID:  record.SessionID,
		ResultRef:  record.ResultRef,
		Status:     record.Status,
		Duplicate:  true,
		Processing: record.Status == platform.IdempotencyStatusProcessing,
	}
	if record.ResultRef == "" ||
		(record.Status != platform.IdempotencyStatusCompleted &&
			record.Status != platform.IdempotencyStatusReplyFailed) {
		return result, nil
	}
	outbound, ok, err := s.outboundStore.Get(ctx, record.ResultRef)
	if err != nil {
		return Result{}, err
	}
	if ok {
		result.Outbound = outbound
	}
	return result, nil
}

func authorizeBinding(binding platform.ChannelBinding, msg platform.InboundMessage) error {
	if !containsAllowed(binding.AllowedUsers, msg.ExternalUserID) {
		return ErrBindingAccessDenied
	}
	if msg.ConversationType != platform.ConversationTypeDM &&
		!containsAllowed(binding.AllowedGroups, msg.ExternalGroupID) {
		return ErrBindingAccessDenied
	}
	if binding.RequiredMention &&
		msg.ConversationType != platform.ConversationTypeDM &&
		!msg.RequiredMentionSeen {
		return ErrBindingMentionRequired
	}
	return nil
}

func containsAllowed(allowed []string, value string) bool {
	if len(allowed) == 0 {
		return true
	}
	value = strings.TrimSpace(value)
	for _, candidate := range allowed {
		if strings.TrimSpace(candidate) == value {
			return true
		}
	}
	return false
}

func inboundText(msg platform.InboundMessage) (string, error) {
	if msg.MessageType != platform.MessageTypeText {
		return "", ErrUnsupportedMessageType
	}
	var parts []string
	for _, part := range msg.ContentParts {
		if part.Type != platform.ContentPartTypeText {
			return "", ErrUnsupportedMessageType
		}
		text := strings.TrimSpace(part.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return "", ErrEmptyText
	}
	return text, nil
}

func collectAssistantText(ch <-chan *event.Event) (string, error) {
	var parts []string
	var final string
	for evt := range ch {
		if evt == nil || evt.Response == nil {
			continue
		}
		if evt.IsTerminalError() {
			return "", evt.Response.Error
		}
		if evt.IsRunnerCompletion() {
			break
		}
		if len(evt.Choices) == 0 {
			continue
		}
		for _, choice := range evt.Choices {
			content := choice.Message.Content
			if content == "" {
				content = choice.Delta.Content
			}
			if content != "" {
				if evt.Done && !evt.IsPartial && choice.Message.Content != "" {
					final = content
					continue
				}
				parts = append(parts, content)
			}
		}
	}
	if strings.TrimSpace(final) != "" {
		return strings.TrimSpace(final), nil
	}
	content := strings.TrimSpace(strings.Join(parts, ""))
	if content == "" {
		return "", ErrRunnerResponseEmpty
	}
	return content, nil
}

func requestIDFor(msg platform.InboundMessage) string {
	if requestID := strings.TrimSpace(msg.TraceContext["request_id"]); requestID != "" {
		return requestID
	}
	return platform.IdempotencyKey(
		msg.TenantID,
		msg.Channel,
		msg.ChannelAccountID,
		msg.PlatformMessageID,
	)
}

func auditFromMessage(
	msg platform.InboundMessage,
	sessionID string,
	internalUserID string,
	decision string,
	reason string,
	start time.Time,
	err error,
) platform.AuditRecord {
	record := platform.AuditRecord{
		TenantID:       msg.TenantID,
		AppID:          msg.AppID,
		Channel:        msg.Channel,
		BindingID:      msg.BindingID,
		UserID:         platform.UserIDHash(msg.TenantID, msg.Channel, msg.ExternalUserID),
		InternalUserID: internalUserID,
		UserIDHash:     platform.UserIDHash(msg.TenantID, msg.Channel, msg.ExternalUserID),
		SessionID:      sessionID,
		MessageID:      msg.PlatformMessageID,
		RequestID:      requestIDFor(msg),
		Decision:       decision,
		DecisionReason: redactAuditReason(reason),
		LatencyMS:      time.Since(start).Milliseconds(),
		CreatedAt:      time.Now(),
	}
	if err != nil {
		record.ErrorType = fmt.Sprintf("%T", err)
	}
	return record
}

func redactAuditReason(reason string) string {
	if reason == "" {
		return ""
	}
	redactor, err := platform.NewRedactor()
	if err != nil {
		return reason
	}
	return redactor.Redact(reason)
}

func (s *Service) writeAudit(ctx context.Context, record platform.AuditRecord) {
	if s.auditSink == nil {
		return
	}
	_ = s.auditSink.WriteAudit(ctx, record)
}

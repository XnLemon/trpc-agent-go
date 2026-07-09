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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/platform/channeladapter"
	telemetrytrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// Service handles normalized inbound platform messages.
type Service struct {
	registry         Registry
	idempotencyStore platform.IdempotencyStore
	outboundStore    OutboundStore
	leaseStore       SessionLeaseStore
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

// WithSessionLeaseStore sets the lease store used to serialize same-session runs.
func WithSessionLeaseStore(store SessionLeaseStore) Option {
	return func(s *Service) {
		s.leaseStore = store
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
		leaseStore:       NewInMemorySessionLeaseStore(),
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
	ctx, callbackSpan := telemetrytrace.Tracer.Start(ctx, "im.callback")
	defer callbackSpan.End()
	setInboundTraceAttributes(callbackSpan, msg, "", "", "")
	if err := s.validateService(); err != nil {
		recordSpanError(callbackSpan, err)
		return Result{}, err
	}
	if err := msg.Validate(); err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		recordSpanError(callbackSpan, err)
		return Result{}, err
	}
	requestID := requestIDFor(msg)
	setInboundTraceAttributes(callbackSpan, msg, "", requestID, "")
	routeCtx, routeSpan := telemetrytrace.Tracer.Start(ctx, "gateway.route")
	defer routeSpan.End()
	setInboundTraceAttributes(routeSpan, msg, "", requestID, "")
	runtime, ok, err := s.registry.Lookup(routeCtx, msg)
	if err != nil {
		recordSpanError(routeSpan, err)
		return Result{}, err
	}
	if !ok {
		err := ErrRuntimeNotFound
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		recordSpanError(routeSpan, err)
		return Result{}, err
	}
	if err := runtime.Validate(); err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		recordSpanError(routeSpan, err)
		return Result{}, err
	}
	if !runtime.matchesInbound(msg) {
		err := ErrRuntimeMismatch
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		recordSpanError(routeSpan, err)
		return Result{}, err
	}
	if err := authorizeBinding(runtime.Binding, msg); err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		return Result{}, err
	}
	text, err := inboundText(msg)
	if err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
		recordSpanError(routeSpan, err)
		return Result{}, err
	}
	sessionID, err := platform.SessionIDForInbound(msg)
	if err != nil {
		recordSpanError(routeSpan, err)
		return Result{}, err
	}
	internalUserID := platform.InternalUserID(msg.TenantID, msg.Channel, msg.ExternalUserID)
	setInboundTraceAttributes(callbackSpan, msg, sessionID, requestID, internalUserID)
	setInboundTraceAttributes(routeSpan, msg, sessionID, requestID, internalUserID)
	key := platform.IdempotencyKey(
		msg.TenantID,
		msg.Channel,
		msg.ChannelAccountID,
		msg.PlatformMessageID,
	)
	idempotencyCtx, idempotencySpan := telemetrytrace.Tracer.Start(routeCtx, "gateway.idempotency")
	setInboundTraceAttributes(idempotencySpan, msg, sessionID, requestID, internalUserID)
	existing, ok, err := s.idempotencyStore.Get(idempotencyCtx, key)
	if err != nil {
		recordSpanError(idempotencySpan, err)
		idempotencySpan.End()
		return Result{}, err
	}
	if ok {
		idempotencySpan.End()
		return s.duplicateResult(ctx, existing)
	}
	leaseCtx, leaseSpan := telemetrytrace.Tracer.Start(routeCtx, "gateway.session_lock")
	setInboundTraceAttributes(leaseSpan, msg, sessionID, requestID, internalUserID)
	lease, acquired, err := s.leaseStore.Acquire(leaseCtx, SessionLeaseKey{
		TenantID:  msg.TenantID,
		AppID:     msg.AppID,
		SessionID: sessionID,
	})
	if err != nil {
		recordSpanError(leaseSpan, err)
		leaseSpan.End()
		return Result{}, err
	}
	if !acquired {
		leaseSpan.End()
		return Result{
			RequestID:  requestID,
			SessionID:  sessionID,
			Status:     platform.IdempotencyStatusProcessing,
			Processing: true,
		}, nil
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = lease.Release(cleanupCtx)
	}()
	leaseSpan.End()
	record, started, err := s.idempotencyStore.Start(idempotencyCtx, platform.IdempotencyRecord{
		TenantID:          msg.TenantID,
		Channel:           msg.Channel,
		AccountID:         msg.ChannelAccountID,
		PlatformMessageID: msg.PlatformMessageID,
		IdempotencyKey:    key,
		RequestID:         requestID,
		SessionID:         sessionID,
	})
	if err != nil {
		recordSpanError(idempotencySpan, err)
		idempotencySpan.End()
		return Result{}, err
	}
	if !started {
		idempotencySpan.End()
		return s.duplicateResult(ctx, record)
	}
	idempotencySpan.End()

	runnerCtx, runnerSpan := telemetrytrace.Tracer.Start(routeCtx, "runner.run")
	setInboundTraceAttributes(runnerSpan, msg, sessionID, requestID, internalUserID)
	ch, err := runtime.Runner.Run(
		runnerCtx,
		internalUserID,
		sessionID,
		model.NewUserMessage(text),
		agent.WithRequestID(requestID),
		agent.WithLatencyDiagnostics(true),
		agent.WithLatencyDiagnosticsEvents(false),
	)
	if err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, sessionID, internalUserID, "runner_error", err.Error(), start, err))
		recordSpanError(runnerSpan, err)
		runnerSpan.End()
		return Result{}, err
	}
	content, err := collectAssistantText(ctx, ch)
	if err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, sessionID, internalUserID, "runner_error", err.Error(), start, err))
		recordSpanError(runnerSpan, err)
		runnerSpan.End()
		return Result{}, err
	}
	runnerSpan.End()
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
	replyCtx, replySpan := telemetrytrace.Tracer.Start(routeCtx, "im.reply")
	setInboundTraceAttributes(replySpan, msg, sessionID, requestID, internalUserID)
	if err := s.outboundStore.Save(replyCtx, resultRef, outbound); err != nil {
		s.writeAudit(ctx, auditFromMessage(msg, sessionID, internalUserID, "outbound_error", err.Error(), start, err))
		recordSpanError(replySpan, err)
		replySpan.End()
		return Result{}, err
	}
	if err := s.outboundStore.Enqueue(
		replyCtx,
		outbound,
		channeladapter.RetryPolicyForBinding(runtime.Binding),
	); err != nil {
		if _, markErr := s.idempotencyStore.MarkReplyFailed(replyCtx, key, resultRef); markErr != nil {
			recordSpanError(replySpan, markErr)
			replySpan.End()
			return Result{}, markErr
		}
		s.writeAudit(ctx, auditFromMessage(msg, sessionID, internalUserID, "outbound_error", err.Error(), start, err))
		recordSpanError(replySpan, err)
		replySpan.End()
		return Result{}, err
	}
	record, err = s.idempotencyStore.Complete(replyCtx, key, resultRef)
	if err != nil {
		recordSpanError(replySpan, err)
		replySpan.End()
		return Result{}, err
	}
	replySpan.End()
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
	if s.leaseStore == nil {
		return fmt.Errorf("gateway session lease store is required")
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

func collectAssistantText(ctx context.Context, ch <-chan *event.Event) (string, error) {
	var parts []string
	var final string
	for {
		var evt *event.Event
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case next, ok := <-ch:
			if !ok {
				goto done
			}
			evt = next
		}
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
done:
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
		AuditID:        platform.AuditID(msg.TenantID, msg.AppID, msg.Channel, msg.BindingID, msg.PlatformMessageID, sessionID, decision),
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
		TraceID:        requestIDFor(msg),
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

func setInboundTraceAttributes(
	span interface{ SetAttributes(...attribute.KeyValue) },
	msg platform.InboundMessage,
	sessionID string,
	requestID string,
	internalUserID string,
) {
	attrs := []attribute.KeyValue{
		attribute.String("tenant_id", msg.TenantID),
		attribute.String("app_id", msg.AppID),
		attribute.String("channel", msg.Channel),
		attribute.String("binding_id", msg.BindingID),
		attribute.String("request_id_hash", traceSafeHash("request", requestID)),
		attribute.String("user_id", platform.UserIDHash(msg.TenantID, msg.Channel, msg.ExternalUserID)),
		attribute.String("user_id_hash", platform.UserIDHash(msg.TenantID, msg.Channel, msg.ExternalUserID)),
	}
	if sessionID != "" {
		attrs = append(attrs, attribute.String("session_id_hash", traceSafeHash("session", sessionID)))
	}
	if internalUserID != "" {
		attrs = append(attrs, attribute.String("internal_user_id_hash", traceSafeHash("internal_user", internalUserID)))
	}
	span.SetAttributes(attrs...)
}

func traceSafeHash(scope string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(scope + "\x00" + value))
	return scope + "_hash_" + hex.EncodeToString(sum[:])[:24]
}

func recordSpanError(span oteltrace.Span, err error) {
	if err == nil {
		return
	}
	errType := traceErrorType(err)
	span.RecordError(errors.New(errType))
	span.SetAttributes(attribute.String("error.type", errType))
	span.SetStatus(codes.Error, errType)
}

func traceErrorType(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	case errors.Is(err, platform.ErrTenantIDRequired):
		return "tenant_id_required"
	case errors.Is(err, platform.ErrAppIDRequired):
		return "app_id_required"
	case errors.Is(err, platform.ErrBindingIDRequired):
		return "binding_id_required"
	case errors.Is(err, platform.ErrChannelRequired):
		return "channel_required"
	case errors.Is(err, platform.ErrAccountIDRequired):
		return "account_id_required"
	case errors.Is(err, platform.ErrPlatformMessageIDRequired):
		return "platform_message_id_required"
	case errors.Is(err, platform.ErrExternalUserIDRequired):
		return "external_user_id_required"
	case errors.Is(err, platform.ErrExternalGroupIDRequired):
		return "external_group_id_required"
	case errors.Is(err, platform.ErrConversationTypeRequired):
		return "conversation_type_required"
	case errors.Is(err, platform.ErrInvalidConversationType):
		return "invalid_conversation_type"
	case errors.Is(err, ErrRuntimeNotFound):
		return "runtime_not_found"
	case errors.Is(err, ErrRuntimeInactive):
		return "runtime_inactive"
	case errors.Is(err, ErrRuntimeMismatch):
		return "runtime_mismatch"
	case errors.Is(err, ErrBindingAccessDenied):
		return "binding_access_denied"
	case errors.Is(err, ErrBindingMentionRequired):
		return "binding_mention_required"
	case errors.Is(err, ErrUnsupportedMessageType):
		return "unsupported_message_type"
	case errors.Is(err, ErrEmptyText):
		return "empty_text"
	case errors.Is(err, ErrRunnerResponseEmpty):
		return "runner_response_empty"
	default:
		return "gateway_error"
	}
}

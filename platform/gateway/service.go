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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	itelemetry "trpc.group/trpc-go/trpc-agent-go/internal/telemetry"
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
	messageEventSink platform.MessageEventSink
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

// WithMessageEventSink sets the message event sink used by the service.
func WithMessageEventSink(sink platform.MessageEventSink) Option {
	return func(s *Service) {
		s.messageEventSink = sink
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
	runtime, err := s.lookupRuntime(routeCtx, ctx, routeSpan, msg, start)
	if err != nil {
		return Result{}, err
	}
	auditSink := s.auditSinkForRuntime(runtime)
	text, err := s.validateInboundContent(ctx, routeSpan, msg, start, auditSink)
	if err != nil {
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
	record, handled, result, err := s.startInboundRun(
		routeCtx,
		ctx,
		msg,
		sessionID,
		requestID,
		internalUserID,
		key,
	)
	if err != nil {
		return Result{}, err
	}
	if handled {
		return result, nil
	}
	defer s.releaseSessionLease(ctx, record.SessionLease)

	return s.runAndReply(
		routeCtx,
		ctx,
		runtime,
		auditSink,
		msg,
		inboundRunInput{
			Text:           text,
			SessionID:      sessionID,
			InternalUserID: internalUserID,
			RequestID:      requestID,
			Key:            key,
			FencingToken:   record.SessionLease.FencingToken(),
			Start:          start,
		},
	)
}

type inboundRunRecord struct {
	Record       platform.IdempotencyRecord
	SessionLease SessionLease
}

type inboundRunInput struct {
	Text           string
	SessionID      string
	InternalUserID string
	RequestID      string
	Key            string
	FencingToken   int64
	Start          time.Time
}

func (s *Service) lookupRuntime(
	routeCtx context.Context,
	auditCtx context.Context,
	routeSpan oteltrace.Span,
	msg platform.InboundMessage,
	start time.Time,
) (Runtime, error) {
	runtime, ok, err := s.registry.Lookup(routeCtx, msg)
	if err != nil {
		recordSpanError(routeSpan, err)
		return Runtime{}, err
	}
	if !ok {
		err := ErrRuntimeNotFound
		s.writeRejectAudit(auditCtx, msg, start, err)
		recordSpanError(routeSpan, err)
		return Runtime{}, err
	}
	if err := validateRuntimeForMessage(runtime, msg); err != nil {
		s.writeRejectAudit(auditCtx, msg, start, err)
		recordSpanError(routeSpan, err)
		return Runtime{}, err
	}
	if err := authorizeBinding(runtime.Binding, msg); err != nil {
		s.writeRejectAuditTo(
			auditCtx,
			s.auditSinkForRuntime(runtime),
			msg,
			start,
			err,
		)
		recordSpanError(routeSpan, err)
		return Runtime{}, err
	}
	return runtime, nil
}

func validateRuntimeForMessage(runtime Runtime, msg platform.InboundMessage) error {
	if err := runtime.Validate(); err != nil {
		return err
	}
	if !runtime.matchesInbound(msg) {
		return ErrRuntimeMismatch
	}
	return nil
}

func (s *Service) validateInboundContent(
	ctx context.Context,
	routeSpan oteltrace.Span,
	msg platform.InboundMessage,
	start time.Time,
	auditSink platform.AuditSink,
) (string, error) {
	text, err := inboundText(msg)
	if err != nil {
		s.writeRejectAuditTo(ctx, auditSink, msg, start, err)
		recordSpanError(routeSpan, err)
		return "", err
	}
	return text, nil
}

func (s *Service) startInboundRun(
	routeCtx context.Context,
	resultCtx context.Context,
	msg platform.InboundMessage,
	sessionID string,
	requestID string,
	internalUserID string,
	key string,
) (inboundRunRecord, bool, Result, error) {
	idempotencyCtx, idempotencySpan := telemetrytrace.Tracer.Start(routeCtx, "gateway.idempotency")
	defer idempotencySpan.End()
	setInboundTraceAttributes(idempotencySpan, msg, sessionID, requestID, internalUserID)
	existing, ok, err := s.idempotencyStore.Get(idempotencyCtx, key)
	if err != nil {
		recordSpanError(idempotencySpan, err)
		return inboundRunRecord{}, false, Result{}, err
	}
	if ok {
		result, err := s.duplicateResult(resultCtx, existing)
		return inboundRunRecord{}, true, result, err
	}
	return s.acquireSessionLeaseAndStart(
		routeCtx,
		resultCtx,
		idempotencyCtx,
		idempotencySpan,
		msg,
		sessionID,
		requestID,
		internalUserID,
		key,
	)
}

func (s *Service) acquireSessionLeaseAndStart(
	routeCtx context.Context,
	resultCtx context.Context,
	idempotencyCtx context.Context,
	idempotencySpan oteltrace.Span,
	msg platform.InboundMessage,
	sessionID string,
	requestID string,
	internalUserID string,
	key string,
) (inboundRunRecord, bool, Result, error) {
	lease, handled, result, err := s.acquireSessionLease(
		routeCtx,
		msg,
		sessionID,
		requestID,
		internalUserID,
	)
	if err != nil || handled {
		return inboundRunRecord{}, handled, result, err
	}
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
		s.releaseSessionLease(resultCtx, lease)
		recordSpanError(idempotencySpan, err)
		return inboundRunRecord{}, false, Result{}, err
	}
	if !started {
		s.releaseSessionLease(resultCtx, lease)
		result, err := s.duplicateResult(resultCtx, record)
		return inboundRunRecord{}, true, result, err
	}
	return inboundRunRecord{Record: record, SessionLease: lease}, false, Result{}, nil
}

func (s *Service) acquireSessionLease(
	routeCtx context.Context,
	msg platform.InboundMessage,
	sessionID string,
	requestID string,
	internalUserID string,
) (SessionLease, bool, Result, error) {
	leaseCtx, leaseSpan := telemetrytrace.Tracer.Start(routeCtx, "gateway.session_lock")
	defer leaseSpan.End()
	setInboundTraceAttributes(leaseSpan, msg, sessionID, requestID, internalUserID)
	lease, acquired, err := s.leaseStore.Acquire(leaseCtx, SessionLeaseKey{
		TenantID:  msg.TenantID,
		AppID:     msg.AppID,
		SessionID: sessionID,
	})
	if err != nil {
		recordSpanError(leaseSpan, err)
		return nil, false, Result{}, err
	}
	if acquired {
		return lease, false, Result{}, nil
	}
	return nil, true, Result{
		RequestID:  requestID,
		SessionID:  sessionID,
		Status:     platform.IdempotencyStatusProcessing,
		Processing: true,
	}, nil
}

func (s *Service) releaseSessionLease(ctx context.Context, lease SessionLease) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = lease.Release(cleanupCtx)
}

func (s *Service) runAndReply(
	routeCtx context.Context,
	auditCtx context.Context,
	runtime Runtime,
	auditSink platform.AuditSink,
	msg platform.InboundMessage,
	input inboundRunInput,
) (Result, error) {
	content, err := s.runGatewayRunner(
		routeCtx,
		auditCtx,
		runtime,
		auditSink,
		msg,
		input,
	)
	if err != nil {
		return Result{}, err
	}
	return s.writeReply(
		routeCtx,
		auditCtx,
		runtime,
		auditSink,
		msg,
		input,
		content,
	)
}

func (s *Service) runGatewayRunner(
	routeCtx context.Context,
	auditCtx context.Context,
	runtime Runtime,
	auditSink platform.AuditSink,
	msg platform.InboundMessage,
	input inboundRunInput,
) (string, error) {
	runnerCtx, runnerSpan := telemetrytrace.Tracer.Start(routeCtx, "runner.run")
	defer runnerSpan.End()
	runnerCtx = platform.ContextWithStorageFencingToken(runnerCtx, input.FencingToken)
	setInboundTraceAttributes(runnerSpan, msg, input.SessionID, input.RequestID, input.InternalUserID)
	if input.FencingToken > 0 {
		runnerSpan.SetAttributes(attribute.Int64("storage.fencing_token", input.FencingToken))
	}
	ch, err := runtime.Runner.Run(
		runnerCtx,
		input.InternalUserID,
		input.SessionID,
		model.NewUserMessage(input.Text),
		agent.WithRequestID(input.RequestID),
		agent.WithLatencyDiagnostics(true),
		agent.WithLatencyDiagnosticsEvents(false),
	)
	if err != nil {
		s.writeAuditTo(auditCtx, auditSink, auditFromMessage(msg, input.SessionID, input.InternalUserID, "runner_error", err.Error(), input.Start, err))
		recordSpanError(runnerSpan, err)
		return "", err
	}
	content, err := collectAssistantText(auditCtx, ch)
	if err != nil {
		s.writeAuditTo(auditCtx, auditSink, auditFromMessage(msg, input.SessionID, input.InternalUserID, "runner_error", err.Error(), input.Start, err))
		recordSpanError(runnerSpan, err)
		return "", err
	}
	return content, nil
}

func (s *Service) writeReply(
	routeCtx context.Context,
	auditCtx context.Context,
	runtime Runtime,
	auditSink platform.AuditSink,
	msg platform.InboundMessage,
	input inboundRunInput,
	content string,
) (Result, error) {
	reply := newReplyPlan(input.Key, 0)
	outbound := platform.OutboundMessage{
		TenantID:                 msg.TenantID,
		BindingID:                msg.BindingID,
		Channel:                  msg.Channel,
		SessionID:                input.SessionID,
		ReplyToPlatformMessageID: msg.PlatformMessageID,
		Kind:                     platform.OutboundMessageKindText,
		Content:                  content,
		Sequence:                 reply.OutboundSequence,
		DedupKey:                 reply.ResultRef,
		TraceID:                  input.RequestID,
	}
	replyCtx, replySpan := telemetrytrace.Tracer.Start(routeCtx, "im.reply")
	defer replySpan.End()
	setInboundTraceAttributes(replySpan, msg, input.SessionID, input.RequestID, input.InternalUserID)
	if err := s.outboundStore.Save(replyCtx, reply.ResultRef, outbound); err != nil {
		s.writeAuditTo(auditCtx, auditSink, auditFromMessage(msg, input.SessionID, input.InternalUserID, "outbound_error", err.Error(), input.Start, err))
		recordSpanError(replySpan, err)
		return Result{}, err
	}
	if err := s.outboundStore.Enqueue(
		replyCtx,
		outbound,
		channeladapter.RetryPolicyForBinding(runtime.Binding),
	); err != nil {
		if _, markErr := s.idempotencyStore.MarkReplyFailed(replyCtx, input.Key, reply.ResultRef); markErr != nil {
			recordSpanError(replySpan, markErr)
			return Result{}, markErr
		}
		s.writeAuditTo(auditCtx, auditSink, auditFromMessage(msg, input.SessionID, input.InternalUserID, "outbound_error", err.Error(), input.Start, err))
		recordSpanError(replySpan, err)
		return Result{}, err
	}
	record, err := s.idempotencyStore.Complete(replyCtx, input.Key, reply.ResultRef)
	if err != nil {
		recordSpanError(replySpan, err)
		return Result{}, err
	}
	s.writeMessageEvent(auditCtx, messageEventFromInbound(msg, input.SessionID, input.Key, input.RequestID, reply.InboundSequence, input.Start))
	s.writeMessageEvent(auditCtx, messageEventFromAssistant(msg, input.SessionID, reply.ResultRef, input.RequestID, reply.AssistantSequence, s.now()))
	s.writeAuditTo(auditCtx, auditSink, auditFromMessage(msg, input.SessionID, input.InternalUserID, "completed", "", input.Start, nil))
	return Result{
		RequestID:   input.RequestID,
		SessionID:   input.SessionID,
		ResultRef:   reply.ResultRef,
		Status:      record.Status,
		Outbound:    outbound,
		CompletedAt: s.now(),
	}, nil
}

type replyPlan struct {
	ResultRef         string
	InboundSequence   int64
	OutboundSequence  int
	AssistantSequence int64
}

func newReplyPlan(idempotencyKey string, outboundIndex int) replyPlan {
	outboundSequence := outboundIndex + 1
	inboundSequence := int64(1)
	return replyPlan{
		ResultRef:         fmt.Sprintf("%s:outbound:%d", idempotencyKey, outboundSequence),
		InboundSequence:   inboundSequence,
		OutboundSequence:  outboundSequence,
		AssistantSequence: inboundSequence + int64(outboundSequence),
	}
}

func (s *Service) writeRejectAudit(
	ctx context.Context,
	msg platform.InboundMessage,
	start time.Time,
	err error,
) {
	s.writeAudit(ctx, auditFromMessage(msg, "", "", "reject", err.Error(), start, err))
}

func (s *Service) writeRejectAuditTo(
	ctx context.Context,
	auditSink platform.AuditSink,
	msg platform.InboundMessage,
	start time.Time,
	err error,
) {
	s.writeAuditTo(
		ctx,
		auditSink,
		auditFromMessage(msg, "", "", "reject", err.Error(), start, err),
	)
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
	s.writeAuditTo(ctx, s.auditSink, record)
}

func (s *Service) writeAuditTo(
	ctx context.Context,
	auditSink platform.AuditSink,
	record platform.AuditRecord,
) {
	if isNilAuditSink(auditSink) {
		return
	}
	_ = auditSink.WriteAudit(ctx, record)
}

func (s *Service) auditSinkForRuntime(runtime Runtime) platform.AuditSink {
	if !isNilAuditSink(runtime.Audit) {
		return runtime.Audit
	}
	return s.auditSink
}

func isNilAuditSink(auditSink platform.AuditSink) bool {
	if auditSink == nil {
		return true
	}
	reflected := reflect.ValueOf(auditSink)
	switch reflected.Kind() {
	case reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.Map,
		reflect.Pointer,
		reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (s *Service) writeMessageEvent(ctx context.Context, event platform.MessageEvent) {
	if s.messageEventSink == nil {
		return
	}
	_ = s.messageEventSink.WriteMessageEvent(ctx, event)
}

func messageEventFromInbound(
	msg platform.InboundMessage,
	sessionID string,
	idempotencyKey string,
	traceID string,
	sequence int64,
	createdAt time.Time,
) platform.MessageEvent {
	return platform.MessageEvent{
		TenantID:       msg.TenantID,
		AppID:          msg.AppID,
		SessionID:      sessionID,
		EventID:        idempotencyKey + ":user",
		Sequence:       sequence,
		IdempotencyKey: idempotencyKey,
		Role:           platform.MessageEventRoleUser,
		EventType:      platform.MessageEventTypeMessage,
		TraceID:        traceID,
		CreatedAt:      createdAt,
	}
}

func messageEventFromAssistant(
	msg platform.InboundMessage,
	sessionID string,
	resultRef string,
	traceID string,
	sequence int64,
	createdAt time.Time,
) platform.MessageEvent {
	return platform.MessageEvent{
		TenantID:       msg.TenantID,
		AppID:          msg.AppID,
		SessionID:      sessionID,
		EventID:        resultRef + ":assistant",
		Sequence:       sequence,
		IdempotencyKey: resultRef,
		Role:           platform.MessageEventRoleAssistant,
		EventType:      platform.MessageEventTypeMessage,
		TraceID:        traceID,
		CreatedAt:      createdAt,
	}
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
	return itelemetry.TraceSafeHash(scope, value)
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
	if err == nil {
		return ""
	}
	for _, candidate := range traceErrorTypes {
		if errors.Is(err, candidate.err) {
			return candidate.name
		}
	}
	return "gateway_error"
}

var traceErrorTypes = []struct {
	err  error
	name string
}{
	{context.Canceled, "context_canceled"},
	{context.DeadlineExceeded, "context_deadline_exceeded"},
	{platform.ErrTenantIDRequired, "tenant_id_required"},
	{platform.ErrAppIDRequired, "app_id_required"},
	{platform.ErrBindingIDRequired, "binding_id_required"},
	{platform.ErrChannelRequired, "channel_required"},
	{platform.ErrAccountIDRequired, "account_id_required"},
	{platform.ErrPlatformMessageIDRequired, "platform_message_id_required"},
	{platform.ErrExternalUserIDRequired, "external_user_id_required"},
	{platform.ErrExternalGroupIDRequired, "external_group_id_required"},
	{platform.ErrConversationTypeRequired, "conversation_type_required"},
	{platform.ErrInvalidConversationType, "invalid_conversation_type"},
	{ErrRuntimeNotFound, "runtime_not_found"},
	{ErrRuntimeInactive, "runtime_inactive"},
	{ErrRuntimeMismatch, "runtime_mismatch"},
	{ErrBindingAccessDenied, "binding_access_denied"},
	{ErrBindingMentionRequired, "binding_mention_required"},
	{ErrUnsupportedMessageType, "unsupported_message_type"},
	{ErrEmptyText, "empty_text"},
	{ErrRunnerResponseEmpty, "runner_response_empty"},
}

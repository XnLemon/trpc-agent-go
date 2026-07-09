//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	itelemetry "trpc.group/trpc-go/trpc-agent-go/internal/telemetry"
	"trpc.group/trpc-go/trpc-agent-go/session"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

func setupTracingProvider(t *testing.T) (*tracetest.InMemoryExporter, func()) {
	t.Helper()

	origTracer := atrace.Tracer
	origProvider := atrace.TracerProvider

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	atrace.TracerProvider = tp
	atrace.Tracer = tp.Tracer("test")

	cleanup := func() {
		_ = tp.Shutdown(context.Background())
		atrace.Tracer = origTracer
		atrace.TracerProvider = origProvider
		otel.SetTracerProvider(origProvider)
	}

	return exporter, cleanup
}

func findSpan(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	return nil
}

func spanAttr(s *tracetest.SpanStub, key string) string {
	for _, a := range s.Attributes {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

func TestCreateSessionSummary_WithTracing(t *testing.T) {
	exporter, cleanupTP := setupTracingProvider(t)
	defer cleanupTP()

	summarizer := &mockSummarizerImpl{
		summaryText:     "traced summary",
		shouldSummarize: true,
	}
	s, mock, db := setupMockService(t, &TestServiceOpts{summarizer: summarizer})
	defer db.Close()

	sess := &session.Session{
		ID:        "trace-session",
		AppName:   "trace-app",
		UserID:    "trace-user",
		UpdatedAt: time.Now(),
	}

	mock.ExpectExec("INSERT INTO session_summaries").
		WithArgs("trace-app", "trace-user", "trace-session", "", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := s.CreateSessionSummary(context.Background(), sess, "", true)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())

	span := findSpan(exporter.GetSpans(), "create_session_summary")
	require.NotNil(t, span, "expected create_session_summary span")
	assert.Equal(t, itelemetry.OperationSummaryCreate, spanAttr(span, semconvtrace.KeyTRPCAgentGoTraceSpan))
}

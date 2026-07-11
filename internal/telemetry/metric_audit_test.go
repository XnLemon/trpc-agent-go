//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package telemetry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/metrics"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
)

func TestReportAuditWriteFailedMetricsNoopWhenCounterNil(t *testing.T) {
	originalCounter := AuditMetricWriteFailedTotal
	t.Cleanup(func() {
		AuditMetricWriteFailedTotal = originalCounter
	})

	AuditMetricWriteFailedTotal = nil
	require.NotPanics(t, func() {
		ReportAuditWriteFailedMetrics(context.Background(), AuditAttributes{
			TenantID: "tenant-1",
			AppName:  "app-1",
			Decision: "reject",
			Error:    errors.New("audit unavailable"),
		})
	})
}

func TestReportAuditWriteFailedMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	originalProvider := MeterProvider
	originalMeter := AuditMeter
	originalCounter := AuditMetricWriteFailedTotal
	t.Cleanup(func() {
		MeterProvider = originalProvider
		AuditMeter = originalMeter
		AuditMetricWriteFailedTotal = originalCounter
	})

	MeterProvider = provider
	AuditMeter = provider.Meter(metrics.MeterNameAudit)
	var err error
	AuditMetricWriteFailedTotal, err = AuditMeter.Int64Counter(metrics.MetricAuditWriteFailedTotal)
	require.NoError(t, err)

	ctx := context.Background()
	ReportAuditWriteFailedMetrics(ctx, AuditAttributes{
		TenantID: "tenant-1",
		AppName:  "app-1",
		Decision: "reject",
		Error:    errors.New("audit unavailable"),
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	points := auditSumPoints(t, rm, metrics.MetricAuditWriteFailedTotal)
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[0].Value)
	requireAuditAttr(t, points[0].Attributes, semconvtrace.KeyGenAIOperationName, OperationAuditWrite)
	requireAuditAttr(t, points[0].Attributes, semconvtrace.KeyGenAISystem, semconvtrace.SystemTRPCGoAgent)
	requireAuditAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoTenantID, "tenant-1")
	requireAuditAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoAppName, "app-1")
	requireAuditAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoAuditDecision, "reject")
	requireAuditAttr(t, points[0].Attributes, semconvtrace.KeyErrorType, semconvtrace.ValueDefaultErrorType)
}

func auditSumPoints(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, scopeMetric := range rm.ScopeMetrics {
		for _, metric := range scopeMetric.Metrics {
			if metric.Name != metricName {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			return sum.DataPoints
		}
	}
	t.Fatalf("metric %s not found", metricName)
	return nil
}

func requireAuditAttr(t *testing.T, set attribute.Set, key string, value string) {
	t.Helper()
	for _, kv := range set.ToSlice() {
		if string(kv.Key) == key {
			require.Equal(t, value, kv.Value.AsString())
			return
		}
	}
	t.Fatalf("attribute %s not found", key)
}

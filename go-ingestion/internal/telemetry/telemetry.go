// Package telemetry は OpenTelemetry SDK の初期化を行う。
// go 1.24 の制約上、stdout exporter のみサポート。
// OTLP は go 1.25 以降で go get go.opentelemetry.io/otel/exporters/otlp/... を追加して切り替える。
package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init は OTel SDK を初期化する。
// endpoint は将来の OTLP 切り替え用に受け取るが現在は stdout のみ。
// 返す shutdown 関数はプログラム終了前に呼び出すこと。
func Init(ctx context.Context, _ string) (shutdown func(), err error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName("dataguard-rail")),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	traceExp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, fmt.Errorf("stdout trace exporter: %w", err)
	}
	metricExp, err := stdoutmetric.New()
	if err != nil {
		return nil, fmt.Errorf("stdout metric exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExp)),
		metric.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	InitMetrics()

	return func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	}, nil
}

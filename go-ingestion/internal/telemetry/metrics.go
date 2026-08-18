package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

var violationCounter metric.Int64Counter

func init() {
	// OTel 未初期化時は noop を使用。Init() 呼び出し後は otel.GetMeterProvider() が正しく返る。
	violationCounter, _ = noop.NewMeterProvider().Meter("").Int64Counter("")
}

// InitMetrics は OTel MeterProvider 初期化後に呼び出してカウンタを再取得する。
func InitMetrics() {
	meter := otel.GetMeterProvider().Meter("dataguard-rail")
	c, err := meter.Int64Counter(
		"dataguard.violations.total",
		metric.WithDescription("Total number of data quality violations detected"),
		metric.WithUnit("{violation}"),
	)
	if err == nil {
		violationCounter = c
	}
}

// RecordIngest は ingest 完了時に violation 数を記録する。
func RecordIngest(ctx context.Context, sourceName, sourceType string, count int64) {
	violationCounter.Add(ctx, count,
		metric.WithAttributes(
			attribute.String("source.name", sourceName),
			attribute.String("source.type", sourceType),
		),
	)
}

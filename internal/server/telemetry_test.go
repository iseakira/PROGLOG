package server

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// setupTelemetry は -debug / -otlp フラグに応じて、トレース・メトリクス・ログの
// 出力先を組み立てて cfg に設定する。戻り値の関数で溜まっているデータを書き出して止める。
//
//	-debug: トレースとメトリクスを一時ファイルに、ログを標準エラー出力に書き出す
//	-otlp:  OTLP で localhost:4317(または OTEL_EXPORTER_OTLP_ENDPOINT)に送る
func setupTelemetry(t *testing.T, cfg *Config) (shutdown func()) {
	t.Helper()

	if !*debug && !*otlp {
		// 観測しないときはログを捨てる(テスト出力を汚さないため)
		cfg.Logger = slog.New(slog.DiscardHandler)
		return func() {}
	}

	ctx := context.Background()

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(attribute.String("service.name", "proglog")),
	)
	require.NoError(t, err)

	traceOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	}
	metricOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	logOpts := []sdklog.LoggerProviderOption{sdklog.WithResource(res)}
	var handlers []slog.Handler

	if *debug {
		tracesLogFile, err := os.CreateTemp("", "traces-*.log")
		require.NoError(t, err)
		t.Logf("traces log file: %s", tracesLogFile.Name())

		metricsLogFile, err := os.CreateTemp("", "metrics-*.log")
		require.NoError(t, err)
		t.Logf("metrics log file: %s", metricsLogFile.Name())

		traceExp, err := stdouttrace.New(
			stdouttrace.WithWriter(tracesLogFile),
			stdouttrace.WithPrettyPrint(),
		)
		require.NoError(t, err)
		metricExp, err := stdoutmetric.New(
			stdoutmetric.WithWriter(metricsLogFile),
			stdoutmetric.WithPrettyPrint(),
		)
		require.NoError(t, err)

		traceOpts = append(traceOpts, sdktrace.WithSyncer(traceExp))
		metricOpts = append(metricOpts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(metricExp),
		))
		handlers = append(handlers, slog.NewTextHandler(
			os.Stderr,
			&slog.HandlerOptions{Level: slog.LevelDebug},
		))
	}

	if *otlp {
		traceExp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure())
		require.NoError(t, err)
		metricExp, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithInsecure())
		require.NoError(t, err)
		logExp, err := otlploggrpc.New(ctx, otlploggrpc.WithInsecure())
		require.NoError(t, err)

		traceOpts = append(traceOpts, sdktrace.WithBatcher(traceExp))
		metricOpts = append(metricOpts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(time.Second)),
		))
		logOpts = append(logOpts, sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)))
	}

	tp := sdktrace.NewTracerProvider(traceOpts...)
	mp := sdkmetric.NewMeterProvider(metricOpts...)
	lp := sdklog.NewLoggerProvider(logOpts...)
	if *otlp {
		handlers = append(handlers, otelslog.NewHandler(
			"proglog",
			otelslog.WithLoggerProvider(lp),
		))
	}

	cfg.TracerProvider = tp
	cfg.MeterProvider = mp
	cfg.Logger = slog.New(slog.NewMultiHandler(handlers...))

	return func() {
		// Shutdown は溜まっているデータを書き出してから止めるので、sleep は不要
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for name, shutdown := range map[string]func(context.Context) error{
			"tracer": tp.Shutdown,
			"meter":  mp.Shutdown,
			"logger": lp.Shutdown,
		} {
			if err := shutdown(ctx); err != nil {
				t.Logf("failed to shutdown %s provider: %v", name, err)
			}
		}
	}
}

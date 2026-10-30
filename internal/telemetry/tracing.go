package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func InitTracing(ctx context.Context, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	traceEndpoint := os.Getenv("OTEL_TRACES_ENDPOINT")
	if traceEndpoint == "" {
		traceEndpoint = "http://localhost:10428"
	}
	traceExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(traceEndpoint+"/insert/opentelemetry/v1/traces"),
	)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp, nil
}

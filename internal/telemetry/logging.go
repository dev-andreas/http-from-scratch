package telemetry

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

func InitLogging(ctx context.Context, res *resource.Resource) (*log.LoggerProvider, error) {

	logEndpoint := os.Getenv("OTEL_LOGS_ENDPOINT")
	if logEndpoint == "" {
		logEndpoint = "http://localhost:9428" // default endpoint if nothing is provided
	}

	logExporter, err := otlploghttp.New(
		ctx,
		otlploghttp.WithEndpointURL(logEndpoint+"/insert/opentelemetry/v1/logs"),
	)

	if err != nil {
		return nil, err
	}

	lp := log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
		log.WithResource(res),
	)

	logger := slog.New(otelslog.NewHandler("go-server", otelslog.WithLoggerProvider(lp)))
	slog.SetDefault(logger)

	return lp, nil
}

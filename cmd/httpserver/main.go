package main

import (
	"context"
	"fmt"
	"httpServer/internal/http1"
	"httpServer/internal/telemetry"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const addr = ":8080"

func main() {
	ctx := context.Background()
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("go-server"),
			attribute.String("environment", "local"),
		),
	)
	if err != nil {
		panic(err)
	}

	tp, err := telemetry.InitTracing(ctx, res)
	if err != nil {
		panic(err)
	}

	lp, err := telemetry.InitLogging(ctx, res)
	if err != nil {
		panic(err)
	}

	defer tp.Shutdown(ctx)
	defer lp.Shutdown(ctx)

	http1.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {

		// increment the counter every time /hello is called
		telemetry.HelloRequestsTotal.Inc()

		// start a trace span
		tr := otel.Tracer("go-server")
		spanCtx, span := tr.Start(r.Context(), "hello-handler")
		defer span.End()

		// do logging
		slog.InfoContext(spanCtx, "handling hello request", slog.String("path", r.URL.Path))

		fmt.Fprintf(w, "Hello world, on Friday afternoon!")
	})

	http1.HandleFunc("/metrics", telemetry.MetricsHandler)

	handler := otelhttp.NewHandler(http1.DefaultServeMux, "http-server")
	if err := http1.ListenAndServe(addr, handler); err != nil {
		panic(err)
	}
}

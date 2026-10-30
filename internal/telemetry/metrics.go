package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var HelloRequestsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hello_requests_total",
	Help: "The total number of requests made to the /hello endpoint",
})

var MetricsHandler = promhttp.Handler().ServeHTTP

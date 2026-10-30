package main

import (
	"fmt"
	"httpServer/internal/http1"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const addr = ":8080"

var helloRequestsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hello_requests_total",
	Help: "The total number of requests made to the /hello endpoint",
})

func main() {
	http1.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {

		helloRequestsTotal.Inc() // increment the counter every time /hello is called
		fmt.Fprintf(w, "hello world")
	})

	http1.HandleFunc("/metrics", promhttp.Handler().ServeHTTP)

	if err := http1.ListenAndServe(addr, nil); err != nil {
		panic(err)
	}
}

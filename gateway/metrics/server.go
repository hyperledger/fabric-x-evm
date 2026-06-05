package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Listen starts an HTTP server that serves /metrics on the given address.
// returns the server so the caller can shut it down. addr can be ":9091" or
// "127.0.0.1:9092". Returns nil if addr is empty (metrics disabled).
func Listen(addr string) (*http.Server, error) {
	if addr == "" {
		return nil, nil
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[metrics] server on %s exited: %v\n", addr, err)
		}
	}()
	// short wait so the listener binds before the caller starts producing metrics
	time.Sleep(50 * time.Millisecond)
	return srv, nil
}

// Shutdown stops the metrics server with a short grace period.
func Shutdown(srv *http.Server) {
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

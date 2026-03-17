package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atcheri/graceful-shutdown-with-orchestrator-go/internal/shutdownorchestrator"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	orchestrator := shutdownorchestrator.NewShutdownOrchestrator(15 * time.Second)

	orchestrator.Register("http-server", 10*time.Second, func(ctx context.Context) error {
		return server.Shutdown(ctx)
	})

	go func() {
		log.Info("server starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutdown signal received")
	if err := orchestrator.Shutdown(log); err != nil {
		log.Error("shutdown completed with errors", "error", err)
		os.Exit(1)
	}

	log.Info("shutdown complete")
}

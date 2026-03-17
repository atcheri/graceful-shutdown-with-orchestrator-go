# Graceful Shutdown Orchestrator

A lightweight Go library for orchestrating multi-phase graceful shutdowns. Register named shutdown phases with individual timeouts, and the orchestrator runs them in order when your process receives a shutdown signal — collecting errors without stopping early.

## How It Works

```
Signal received
      │
      ▼
┌─────────────────────────────────────────────────────┐
│              ShutdownOrchestrator                   │
│  Total timeout: 15s                                 │
│                                                     │
│  Phase 1: "http-server"   (timeout: 10s) ──► fn()  │
│  Phase 2: "db-pool"       (timeout:  5s) ──► fn()  │
│  Phase 3: "message-queue" (timeout:  3s) ──► fn()  │
└─────────────────────────────────────────────────────┘
      │
      ▼
All errors joined and returned
```

- Phases run **sequentially** in registration order
- Each phase gets its **own timeout** (scoped within the total timeout)
- If the **total timeout** is exceeded, remaining phases are skipped
- A **failing phase does not stop** subsequent phases — all errors are collected and returned together via `errors.Join`

## Installation

```bash
go get github.com/atcheri/graceful-shutdown-with-orchestrator-go
```

## Quick Start

```go
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

    server := &http.Server{Addr: ":8080", Handler: mux}

    // 1. Create the orchestrator with a total shutdown budget
    orchestrator := shutdownorchestrator.NewShutdownOrchestrator(15 * time.Second)

    // 2. Register shutdown phases in the order they should run
    orchestrator.Register("http-server", 10*time.Second, func(ctx context.Context) error {
        return server.Shutdown(ctx)
    })

    // 3. Start your server
    go func() {
        log.Info("server starting", "addr", server.Addr)
        if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Error("server error", "error", err)
            os.Exit(1)
        }
    }()

    // 4. Block until SIGINT or SIGTERM
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    // 5. Run all registered shutdown phases
    log.Info("shutdown signal received")
    if err := orchestrator.Shutdown(log); err != nil {
        log.Error("shutdown completed with errors", "error", err)
        os.Exit(1)
    }

    log.Info("shutdown complete")
}
```

## API

### `NewShutdownOrchestrator(total time.Duration) *ShutdownOrchestrator`

Creates a new orchestrator with a total shutdown budget. If all phases combined take longer than `total`, the remaining phases are skipped.

```go
orchestrator := shutdownorchestrator.NewShutdownOrchestrator(15 * time.Second)
```

### `Register(name string, timeout time.Duration, callback func(ctx context.Context) error)`

Registers a shutdown phase. Phases run in the order they are registered.

| Parameter  | Description                                                              |
|------------|--------------------------------------------------------------------------|
| `name`     | Human-readable label used in log output                                  |
| `timeout`  | Maximum time this phase may take (bounded by remaining total timeout)    |
| `callback` | Function to run; receives a context that is cancelled when timeout fires |

```go
orchestrator.Register("db-pool", 5*time.Second, func(ctx context.Context) error {
    return db.Close()
})

orchestrator.Register("message-queue", 3*time.Second, func(ctx context.Context) error {
    return mq.Drain(ctx)
})
```

### `Shutdown(log *slog.Logger) error`

Runs all registered phases sequentially. Returns a joined error of every phase that failed, or `nil` if all phases succeeded.

```go
if err := orchestrator.Shutdown(log); err != nil {
    // one or more phases failed — check individual errors with errors.Is / errors.As
}
```

## Timeout Semantics

Two levels of timeout control shutdown duration:

| Level        | Set via                              | Behaviour                                                   |
|--------------|--------------------------------------|-------------------------------------------------------------|
| Total budget | `NewShutdownOrchestrator(d)`         | Hard cap across all phases; exceeded phases are skipped     |
| Phase budget | `Register(name, d, fn)`             | Per-phase cap; the context passed to `fn` is cancelled at `d` |

A phase's effective timeout is `min(phase timeout, remaining total budget)`.

**Example:** total=15s, phase A=10s, phase B=10s. If phase A takes 9s, phase B gets at most 6s (15−9), not 10s.

## Logging

The orchestrator uses `log/slog` and emits structured log lines at each phase boundary:

```
time=... level=INFO  msg="shutdown phase starting" phase=http-server
time=... level=INFO  msg="shutdown phase complete" phase=http-server
time=... level=ERROR msg="shutdown phase failed"   phase=db-pool error="connection reset"
```

Pass any `*slog.Logger` to `Shutdown` — use `slog.Default()` during tests or pass your application logger in production.

## Multi-Phase Example

A realistic service with HTTP, database, and cache shutdown phases:

```go
orchestrator := shutdownorchestrator.NewShutdownOrchestrator(30 * time.Second)

// Stop accepting new requests first
orchestrator.Register("http-server", 10*time.Second, func(ctx context.Context) error {
    return httpServer.Shutdown(ctx)
})

// Flush pending writes to the database
orchestrator.Register("db-pool", 15*time.Second, func(ctx context.Context) error {
    return db.Pool.Close()
})

// Let the cache client finish its current operations
orchestrator.Register("redis", 5*time.Second, func(ctx context.Context) error {
    return redisClient.Close()
})
```

## Running the Example

```bash
# Start the server
go run ./cmd/main.go

# In another terminal — test the health endpoint
curl http://localhost:8080/health
# {"status":"ok"}

# Gracefully shut down
kill -SIGTERM <pid>
# time=... level=INFO msg="shutdown signal received"
# time=... level=INFO msg="shutdown phase starting" phase=http-server
# time=... level=INFO msg="shutdown phase complete" phase=http-server
# time=... level=INFO msg="shutdown complete"
```

## Running Tests

```bash
go test ./...
```

The test suite covers:

- Phases run in registration order
- A failing phase does not prevent subsequent phases from running
- The total timeout cancels remaining phases when exceeded

## License

MIT

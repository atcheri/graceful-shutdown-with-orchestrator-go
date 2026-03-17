package shutdownorchestrator_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/testutils/require"
	"github.com/stretchr/testify/assert"

	"github.com/atcheri/graceful-shutdown-with-orchestrator-go/internal/shutdownorchestrator"
)

func TestGracefulShutdown(t *testing.T) {
	// arrange
	so := shutdownorchestrator.NewShutdownOrchestrator(10 * time.Second)

	var order []string
	var mu sync.Mutex

	record := func(name string) func(ctx context.Context) error {
		return func(ctx context.Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}

	so.Register("first", time.Second, record("first"))
	so.Register("second", time.Second, record("second"))
	so.Register("third", time.Second, record("third"))

	// act
	err := so.Shutdown(slog.Default())

	// assert
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"first", "second", "third"}, order)
}

func TestShutdownContinuesAfterPhaseError(t *testing.T) {
	// arrange
	so := shutdownorchestrator.NewShutdownOrchestrator(10 * time.Second)

	so.Register("fails", time.Second, func(ctx context.Context) error {
		return errors.New("boom")
	})

	ran := false
	so.Register("still-runs", time.Second, func(ctx context.Context) error {
		ran = true
		return nil
	})

	// act
	err := so.Shutdown(slog.Default())

	// assert
	assert.Error(t, err)
	assert.True(t, ran, "second phase should run even after first fails")
}

func TestShutdownTimesout(t *testing.T) {
	// arrange
	so := shutdownorchestrator.NewShutdownOrchestrator(100 * time.Millisecond)

	so.Register("first", 80*time.Millisecond, func(ctx context.Context) error {
		select {
		case <-time.After(80 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	so.Register("second", 80*time.Millisecond, func(ctx context.Context) error {
		select {
		case <-time.After(80 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	// act
	err := so.Shutdown(slog.Default())

	// assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

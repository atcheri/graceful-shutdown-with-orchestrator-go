package shutdownorchestrator


import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type ShutdownOrchestrator struct {
	mu      sync.Mutex
	phases  []shutdownPhase
	timeout time.Duration
}

type shutdownPhase struct {
	name     string
	timeout  time.Duration
	callback func(ctx context.Context) error
}

func NewShutdownOrchestrator(total time.Duration) *ShutdownOrchestrator {
	return &ShutdownOrchestrator{
		timeout: total,
	}
}

func (s *ShutdownOrchestrator) Register(name string, timeout time.Duration, callback func(ctx context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.phases = append(s.phases, shutdownPhase{
		name:     name,
		timeout:  timeout,
		callback: callback,
	})
}

func (s *ShutdownOrchestrator) Shutdown(log *slog.Logger) error {
	s.mu.Lock()
	phases := s.phases
	s.mu.Unlock()

	totalCtx, totalCancel := context.WithTimeout(context.Background(), s.timeout)
	defer totalCancel()

	var errs []error
	for _, phase := range phases {
		if err := totalCtx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("shutdown timed out: %w", err))
			break
		}

		if err := s.runPhase(totalCtx, phase, log); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (s *ShutdownOrchestrator) runPhase(parent context.Context, phase shutdownPhase, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(parent, phase.timeout)
	defer cancel()

	log.Info("shutdown phase starting", "phase", phase.name)
	if err := phase.callback(ctx); err != nil {
		log.Error("shutdown phase failed", "phase", phase.name, "error", err)
		return fmt.Errorf("phase %s: %w", phase.name, err)
	}
	log.Info("shutdown phase complete", "phase", phase.name)
	return nil
}

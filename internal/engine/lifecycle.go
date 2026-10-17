// internal/engine/lifecycle.go
package engine

import (
	"context"
	"fmt"
)

// Start begins the splitting engine
func (se *SplittingEngine) Start(ctx context.Context) error {
	se.mu.Lock()
	defer se.mu.Unlock()

	if se.running {
		return fmt.Errorf("splitting engine is already running")
	}

	se.logger.Info("Starting splitting engine")

	// Create cancellable context
	se.ctx, se.cancel = context.WithCancel(ctx)
	se.running = true

	// Start the main loop
	go se.runLoop()

	// Initialize state
	se.session.SetState(StateWaitingForStart)
	if se.session.GetRunConfig().DisableAutostart {
		se.logger.Info("Autostart disabled for this run - waiting for manual start")
	} else {
		se.logger.Info("Autostart enabled - waiting for start condition")
	}

	return nil
}

// Stop stops the splitting engine
func (se *SplittingEngine) Stop() error {
	se.mu.Lock()
	defer se.mu.Unlock()

	if !se.running {
		return nil
	}

	se.logger.Info("Stopping splitting engine")

	se.cancel()
	se.running = false

	// Close all subscriber channels
	se.subscriberMu.Lock()
	for ch := range se.splitSubscribers {
		close(ch)
	}
	for ch := range se.statusSubscribers {
		close(ch)
	}
	se.splitSubscribers = make(map[chan SplitEvent]struct{})
	se.statusSubscribers = make(map[chan StatusEvent]struct{})
	se.subscriberMu.Unlock()

	return nil
}

// IsRunning returns whether the engine is running
func (se *SplittingEngine) IsRunning() bool {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.running
}

// handleError handles errors in the engine
func (se *SplittingEngine) handleError(err error) {
	se.logger.WithError(err).Error("Splitting engine error")
	se.session.SetError()
	se.publishStatus(StateError, fmt.Sprintf("Error: %v", err))
}

// internal/engine/engine.go
package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jdharms/sni-autosplitter/internal/config"
	"github.com/jdharms/sni-autosplitter/internal/sni"
	"github.com/sirupsen/logrus"
)

// Primitive manual testing of resetting a MiSTer has a threshold of about 1.25s where it
// will fail to connect to the device after a reset. This default gives an extra second of
// tolerance to account for other transient issues that may arise.
const DefaultErrorTolerance = 2*time.Second + 250*time.Millisecond

// SplittingEngine manages the autosplitting logic and state
type SplittingEngine struct {
	logger    *logrus.Logger
	client    *sni.Client
	device    *sni.Device
	evaluator *ConditionEvaluator
	session   *SplitterSession

	// Configuration
	pollInterval   time.Duration
	errorTolerance time.Duration

	// Runtime state
	running    bool
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	errorStart time.Time

	// Event subscribers
	splitSubscribers  map[chan SplitEvent]struct{}
	statusSubscribers map[chan StatusEvent]struct{}
	subscriberMu      sync.RWMutex
}

// GetSession returns the current splitter session
func (se *SplittingEngine) GetSession() *SplitterSession {
	return se.session
}

// GetCurrentSplitCondition returns the current split's condition details for debugging
func (se *SplittingEngine) GetCurrentSplitCondition() (*config.Split, *SplitState, error) {
	currentSplitName := se.session.GetCurrentSplitName()
	if currentSplitName == "" {
		return nil, nil, fmt.Errorf("no current split")
	}

	split, err := se.session.GetGameConfig().GetSplitByName(currentSplitName)
	if err != nil {
		return nil, nil, err
	}

	splitState := se.session.GetSplitState(currentSplitName)
	return split, splitState, nil
}

// TestCondition tests a specific condition without affecting the run state
func (se *SplittingEngine) TestCondition(split *config.Split) (bool, error) {
	// Create a temporary split state for testing
	tempState := NewSplitState()

	return se.evaluator.EvaluateComplexCondition(se.ctx, se.device.URI, split, tempState)
}

// runLoop is the main engine loop that handles condition checking
func (se *SplittingEngine) runLoop() {
	se.logger.Info("Splitting engine loop started")
	ticker := time.NewTicker(se.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-se.ctx.Done():
			se.logger.Info("Splitting engine loop stopped")
			return
		case <-ticker.C:
			if err := se.tick(); err != nil {
				se.logger.WithError(err).Error("Error in splitting engine tick")
				se.handleError(err)
			}
		}
	}
}

// tick performs one iteration of condition checking
func (se *SplittingEngine) tick() error {
	state := se.session.GetState()

	switch state {
	case StateWaitingForStart:
		return se.checkAutostart()
	case StateRunning:
		return se.checkCurrentSplit()
	case StatePaused, StateCompleted, StateError:
		// No action needed in these states
		return nil
	default:
		return fmt.Errorf("unknown splitter state: %s", state)
	}
}

// checkAutostart checks if the autostart condition is met
func (se *SplittingEngine) checkAutostart() error {
	if se.session.GetRunConfig().DisableAutostart {
		// Autostart is disabled for this run config; only a manual start
		// (e.g. LiveSplit's "Started" event) can leave StateWaitingForStart.
		return nil
	}

	autostart := se.session.GetGameConfig().Autostart
	autostartState := se.session.GetAutostartState()

	result, err := se.evaluateComplexConditionWithTolerance(&autostart, autostartState)
	if err != nil {
		return fmt.Errorf("failed to evaluate autostart condition: %w", err)
	}
	if result {
		se.logger.Info("Autostart condition met - starting run")
		se.session.Start()

		// Handle the start event
		se.logger.Info("Run started")
		se.publishStatus(StateRunning, "Run started")
	}

	return nil
}

// checkCurrentSplit checks if the current split condition is met
func (se *SplittingEngine) checkCurrentSplit() error {
	currentSplitName := se.session.GetCurrentSplitName()
	if currentSplitName == "" {
		// No more splits - run should be completed
		if se.session.GetState() == StateRunning {
			se.session.SetState(StateCompleted)
		}
		return nil
	}

	// Get the split configuration
	split, err := se.session.GetGameConfig().GetSplitByName(currentSplitName)
	if err != nil {
		return fmt.Errorf("failed to get split configuration for '%s': %w", currentSplitName, err)
	}

	// Get the split state for sequential conditions
	splitState := se.session.GetSplitState(currentSplitName)

	// Evaluate the split condition
	// apply tolerance logic for current split
	result, err := se.evaluateComplexConditionWithTolerance(split, splitState)
	if err != nil {
		return fmt.Errorf("failed to evaluate split condition for '%s': %w", currentSplitName, err)
	}

	if result {
		se.logger.WithField("split", currentSplitName).Info("Split condition met")
		return se.triggerSplit()
	}

	return nil
}

// evaluateComplexConditionWithTolerance suppresses errors until they persist beyond the tolerance window
func (se *SplittingEngine) evaluateComplexConditionWithTolerance(split *config.Split, splitState *SplitState) (bool, error) {
	result, err := se.evaluator.EvaluateComplexCondition(se.ctx, se.device.URI, split, splitState)
	if err != nil {
		tol := se.errorTolerance
		if tol == 0 {
			tol = DefaultErrorTolerance
		}

		if se.errorStart.IsZero() {
			se.errorStart = time.Now()
		}
		if time.Since(se.errorStart) > tol {
			// reset so we don’t immediately re-error next tick
			se.errorStart = time.Time{}
			return false, err
		}
		// still within tolerance → suppress
		return false, nil
	}

	// success → reset any previous error timestamp
	se.errorStart = time.Time{}
	return result, nil
}

// triggerSplit triggers a split and handles the transition
func (se *SplittingEngine) triggerSplit() error {
	// Get split info before triggering
	splitName := se.session.GetCurrentSplitName()
	splitIndex := se.session.GetCurrentSplit()

	if !se.session.TriggerSplit() {
		return fmt.Errorf("failed to trigger split")
	}

	// Log and publish the split event
	se.logger.WithFields(logrus.Fields{
		"split_name":  splitName,
		"split_index": splitIndex,
		"total":       se.session.GetTotalSplits(),
	}).Info("Split triggered")

	event := SplitEvent{
		Action:     SplitActionSplit,
		SplitName:  splitName,
		SplitIndex: splitIndex,
		Timestamp:  time.Now(),
	}
	se.publishSplitEvent(event)

	// Check if run is completed
	if se.session.GetState() == StateCompleted {
		se.logger.Info("Run completed!")
		se.publishStatus(StateCompleted, "Run completed")
	}

	return nil
}

// triggerUndoSplit triggers an undo split and handles the transition
func (se *SplittingEngine) triggerUndoSplit() error {
	// Get the split we're about to undo (which is the previous split)
	splitIndex := se.session.GetCurrentSplit() - 1
	if splitIndex < 0 {
		return fmt.Errorf("no split to undo")
	}
	splitName := se.session.GetRunConfig().Splits[splitIndex]

	if !se.session.TriggerUndoSplit() {
		return fmt.Errorf("failed to trigger undo split")
	}

	// Log and publish the undo event
	se.logger.WithFields(logrus.Fields{
		"split_name":  splitName,
		"split_index": splitIndex,
	}).Info("Split undone")

	event := SplitEvent{
		Action:     SplitActionUndo,
		SplitName:  splitName,
		SplitIndex: splitIndex,
		Timestamp:  time.Now(),
	}
	se.publishSplitEvent(event)

	return nil
}

// triggerSkipSplit triggers a skip split and handles the transition
func (se *SplittingEngine) triggerSkipSplit() error {
	// Get split info before triggering
	splitName := se.session.GetCurrentSplitName()
	splitIndex := se.session.GetCurrentSplit()

	if !se.session.TriggerSkipSplit() {
		return fmt.Errorf("failed to trigger skip split")
	}

	// Log and publish the skip event
	se.logger.WithFields(logrus.Fields{
		"split_name":  splitName,
		"split_index": splitIndex,
	}).Info("Split skipped")

	event := SplitEvent{
		Action:     SplitActionSkip,
		SplitName:  splitName,
		SplitIndex: splitIndex,
		Timestamp:  time.Now(),
	}
	se.publishSplitEvent(event)

	return nil
}

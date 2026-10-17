// internal/engine/manual_controls.go
package engine

import (
	"fmt"
)

// ManualStart manually starts the run, bypassing the autostart condition
func (se *SplittingEngine) ManualStart() error {
	if !se.session.CanStart() {
		return fmt.Errorf("cannot start in current state: %s", se.session.GetState())
	}

	se.logger.Info("Manual start triggered")
	se.session.Start()

	se.logger.Info("Run started")
	se.publishStatus(StateRunning, "Run started manually")

	return nil
}

// ManualSplit manually triggers a split
func (se *SplittingEngine) ManualSplit() error {
	if !se.session.CanSplit() {
		return fmt.Errorf("cannot split in current state: %s", se.session.GetState())
	}

	se.logger.Info("Manual split triggered")
	return se.triggerSplit()
}

// ManualUndoSplit manually undoes the last split
func (se *SplittingEngine) ManualUndoSplit() error {
	se.logger.Info("Manual undo split triggered")
	return se.triggerUndoSplit()
}

// ManualSkipSplit manually skips the last split
func (se *SplittingEngine) ManualSkipSplit() error {
	if !se.session.CanSkip() {
		return fmt.Errorf("cannot skip split in current state: %s (likely on final split)", se.session.GetState())
	}

	se.logger.Info("Manual skip split triggered")
	return se.triggerSkipSplit()
}

// ManualReset manually resets the run
func (se *SplittingEngine) ManualReset() error {
	se.logger.Info("Manual reset triggered")
	se.session.Reset()
	se.session.SetState(StateWaitingForStart)

	// Publish reset status
	se.logger.Info("Run reset")
	if se.session.GetRunConfig().DisableAutostart {
		se.publishStatus(StateWaitingForStart, "Run reset - waiting for manual start")
	} else {
		se.publishStatus(StateWaitingForStart, "Run reset - waiting for autostart")
	}

	return nil
}

// PauseEngine pauses the splitting engine
func (se *SplittingEngine) PauseEngine() error {
	if se.session.GetState() != StateRunning {
		return fmt.Errorf("cannot pause in current state: %s", se.session.GetState())
	}

	se.session.Pause()

	// Handle pause event
	se.logger.Info("Run paused")
	se.publishStatus(StatePaused, "Run paused")

	return nil
}

// ResumeEngine resumes the splitting engine
func (se *SplittingEngine) ResumeEngine() error {
	if se.session.GetState() != StatePaused {
		return fmt.Errorf("cannot resume in current state: %s", se.session.GetState())
	}

	se.session.Resume()

	// Handle resume event
	se.logger.Info("Run resumed")
	se.publishStatus(StateRunning, "Run resumed")

	return nil
}

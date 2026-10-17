// internal/engine/state.go
package engine

import (
	"sync"
	"time"

	"github.com/jdharms/sni-autosplitter/internal/config"
)

// SplitState tracks the state of a single split's sequential conditions
type SplitState struct {
	NextStep   int       // Current step in Next sequence (0 = main condition not met yet)
	LastUpdate time.Time // When this state was last updated
	Completed  bool      // Whether this split has been completed
	mu         sync.RWMutex
}

// NewSplitState creates a new split state
func NewSplitState() *SplitState {
	return &SplitState{
		NextStep:   0,
		LastUpdate: time.Now(),
		Completed:  false,
	}
}

// Reset resets the split state to initial conditions
func (ss *SplitState) Reset() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.NextStep = 0
	ss.LastUpdate = time.Now()
	ss.Completed = false
}

// SetCompleted marks the split as completed
func (ss *SplitState) SetCompleted() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.Completed = true
	ss.LastUpdate = time.Now()
}

// SetIncomplete marks the split as incomplete
func (ss *SplitState) SetIncomplete() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.Completed = false
	ss.LastUpdate = time.Now()
}

// IsCompleted returns whether the split has been completed
func (ss *SplitState) IsCompleted() bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.Completed
}

// GetNextStep returns the current step in the Next sequence
func (ss *SplitState) GetNextStep() int {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.NextStep
}

// IncrementNextStep increments the Next step counter
func (ss *SplitState) IncrementNextStep() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.NextStep++
	ss.LastUpdate = time.Now()
}

// TimeSinceUpdate returns the time since the last update
func (ss *SplitState) TimeSinceUpdate() time.Duration {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return time.Since(ss.LastUpdate)
}

// SplitterState represents the overall state of the autosplitter
type SplitterState int

const (
	StateWaitingForStart SplitterState = iota
	StateRunning
	StatePaused
	StateCompleted
	StateError
)

// String returns the string representation of the splitter state
func (s SplitterState) String() string {
	switch s {
	case StateWaitingForStart:
		return "Waiting for Start"
	case StateRunning:
		return "Running"
	case StatePaused:
		return "Paused"
	case StateCompleted:
		return "Completed"
	case StateError:
		return "Error"
	default:
		return "Unknown"
	}
}

// SplitterSession manages the state of an autosplitting session
type SplitterSession struct {
	runConfig      *config.RunConfig
	gameConfig     *config.GameConfig
	state          SplitterState
	currentSplit   int
	splitStates    map[string]*SplitState
	autostartState *SplitState
	startTime      time.Time
	lastSplit      time.Time
	mu             sync.RWMutex
}

// NewSplitterSession creates a new splitter session
func NewSplitterSession(runConfig *config.RunConfig, gameConfig *config.GameConfig) *SplitterSession {
	session := &SplitterSession{
		runConfig:    runConfig,
		gameConfig:   gameConfig,
		state:        StateWaitingForStart,
		currentSplit: 0,
		splitStates:  make(map[string]*SplitState),
	}

	// Initialize split states for all splits in the run
	for _, splitName := range runConfig.Splits {
		session.splitStates[splitName] = NewSplitState()
	}

	// Initialize autostart state
	session.autostartState = NewSplitState()

	return session
}

// GetState returns the current splitter state
func (ss *SplitterSession) GetState() SplitterState {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.state
}

// GetAutostartState returns the state of the autostart condition
func (ss *SplitterSession) GetAutostartState() *SplitState {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.autostartState
}

// SetState changes the splitter state
func (ss *SplitterSession) SetState(newState SplitterState) {
	ss.mu.Lock()
	ss.state = newState
	ss.mu.Unlock()
}

// GetCurrentSplit returns the current split index
func (ss *SplitterSession) GetCurrentSplit() int {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.currentSplit
}

// GetCurrentSplitName returns the name of the current split
func (ss *SplitterSession) GetCurrentSplitName() string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	if ss.currentSplit < 0 || ss.currentSplit >= len(ss.runConfig.Splits) {
		return ""
	}
	return ss.runConfig.Splits[ss.currentSplit]
}

// GetTotalSplits returns the total number of splits
func (ss *SplitterSession) GetTotalSplits() int {
	return len(ss.runConfig.Splits)
}

// GetSplitState returns the state for a specific split
func (ss *SplitterSession) GetSplitState(splitName string) *SplitState {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	if state, exists := ss.splitStates[splitName]; exists {
		return state
	}

	// Create new state if it doesn't exist
	state := NewSplitState()
	ss.mu.RUnlock()
	ss.mu.Lock()
	ss.splitStates[splitName] = state
	ss.mu.Unlock()
	ss.mu.RLock()

	return state
}

// Start begins the autosplitting session
func (ss *SplitterSession) Start() {
	ss.mu.Lock()
	ss.state = StateRunning
	ss.currentSplit = 0
	ss.startTime = time.Now()
	ss.lastSplit = ss.startTime

	// Reset all split states
	for _, state := range ss.splitStates {
		state.Reset()
	}
	ss.mu.Unlock()
}

// TriggerSplit triggers a split and advances to the next one
func (ss *SplitterSession) TriggerSplit() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.state != StateRunning {
		return false
	}

	if ss.currentSplit >= len(ss.runConfig.Splits) {
		return false
	}

	splitName := ss.runConfig.Splits[ss.currentSplit]

	// Mark current split as completed
	if state, exists := ss.splitStates[splitName]; exists {
		state.SetCompleted()
	}

	ss.lastSplit = time.Now()
	ss.currentSplit++

	// Check if this was the final split
	if ss.currentSplit >= len(ss.runConfig.Splits) {
		ss.state = StateCompleted
	}

	return true
}

// TriggerUndoSplit triggers an undo split and returns to the previous split
func (ss *SplitterSession) TriggerUndoSplit() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.state != StateRunning && ss.state != StatePaused {
		return false
	}

	if ss.currentSplit == 0 {
		return false
	}

	ss.currentSplit--

	splitName := ss.runConfig.Splits[ss.currentSplit]

	// Mark current split as incomplete
	if state, exists := ss.splitStates[splitName]; exists {
		state.SetIncomplete()
	}

	ss.lastSplit = time.Now()

	return true
}

// TriggerSkipSplit triggers a skip split and advances to the next one
func (ss *SplitterSession) TriggerSkipSplit() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.state != StateRunning && ss.state != StatePaused {
		return false
	}

	// Check if the next current split would be valid - can't skip if there's nowhere to skip to
	if ss.currentSplit+1 >= len(ss.runConfig.Splits) {
		return false
	}

	splitName := ss.runConfig.Splits[ss.currentSplit]

	// Mark current split as incomplete
	if state, exists := ss.splitStates[splitName]; exists {
		state.SetIncomplete()
	}

	ss.currentSplit++
	ss.lastSplit = time.Now()

	return true
}

// Reset resets the splitter session
func (ss *SplitterSession) Reset() {
	ss.mu.Lock()
	ss.state = StateWaitingForStart
	ss.currentSplit = 0
	ss.startTime = time.Time{}
	ss.lastSplit = time.Time{}

	// Reset all split states
	for _, state := range ss.splitStates {
		state.Reset()
	}
	ss.mu.Unlock()
}

// Pause pauses the splitter session
func (ss *SplitterSession) Pause() {
	ss.mu.Lock()
	if ss.state == StateRunning {
		ss.state = StatePaused
	}
	ss.mu.Unlock()
}

// Resume resumes the splitter session
func (ss *SplitterSession) Resume() {
	ss.mu.Lock()
	if ss.state == StatePaused {
		ss.state = StateRunning
	}
	ss.mu.Unlock()
}

// SetError sets the splitter to error state
func (ss *SplitterSession) SetError() {
	ss.SetState(StateError)
}

// GetElapsedTime returns the time since the run started
func (ss *SplitterSession) GetElapsedTime() time.Duration {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	if ss.startTime.IsZero() {
		return 0
	}
	return time.Since(ss.startTime)
}

// GetTimeSinceLastSplit returns the time since the last split
func (ss *SplitterSession) GetTimeSinceLastSplit() time.Duration {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	if ss.lastSplit.IsZero() {
		return 0
	}
	return time.Since(ss.lastSplit)
}

// GetRunConfig returns the run configuration
func (ss *SplitterSession) GetRunConfig() *config.RunConfig {
	return ss.runConfig
}

// GetGameConfig returns the game configuration
func (ss *SplitterSession) GetGameConfig() *config.GameConfig {
	return ss.gameConfig
}

// IsRunning returns true if the splitter is actively running
func (ss *SplitterSession) IsRunning() bool {
	return ss.GetState() == StateRunning
}

// CanStart returns true if the splitter can be started.
// This is currently only true when the splitter is in the waiting for start state.
func (ss *SplitterSession) CanStart() bool {
	return ss.GetState() == StateWaitingForStart
}

// CanSplit returns true if a split can be triggered
func (ss *SplitterSession) CanSplit() bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.state == StateRunning && ss.currentSplit < len(ss.runConfig.Splits)
}

// CanSkip returns true if a skip can be triggered
func (ss *SplitterSession) CanSkip() bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return (ss.state == StateRunning || ss.state == StatePaused) && ss.currentSplit+1 < len(ss.runConfig.Splits)
}

// GetProgress returns the completion progress as a percentage (0-100)
func (ss *SplitterSession) GetProgress() float64 {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	if len(ss.runConfig.Splits) == 0 {
		return 0
	}

	return float64(ss.currentSplit) / float64(len(ss.runConfig.Splits)) * 100
}

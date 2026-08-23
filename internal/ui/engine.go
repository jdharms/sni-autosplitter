// internal/ui/engine.go
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jdharms/sni-autosplitter/internal/config"
	"github.com/jdharms/sni-autosplitter/internal/engine"
	"github.com/jdharms/sni-autosplitter/internal/livesplit"
	"github.com/jdharms/sni-autosplitter/internal/sni"
	"github.com/sirupsen/logrus"
)

// EngineController manages the splitting engine lifecycle and provides UI feedback
type EngineController struct {
	logger          *logrus.Logger
	cli             *CLI
	engine          *engine.SplittingEngine
	liveSplitServer *livesplit.Server
	enableManualOps bool // Feature flag for manual operations (split, reset, pause, resume)
	liveSplitPort   int
	tlsCert         string
	tlsKey          string
}

// NewEngineController creates a new engine controller
func NewEngineController(logger *logrus.Logger, cli *CLI, enableManualOps bool, liveSplitPort int, tlsCert, tlsKey string) *EngineController {
	return &EngineController{
		logger:          logger,
		cli:             cli,
		enableManualOps: enableManualOps,
		liveSplitPort:   liveSplitPort,
		tlsCert:         tlsCert,
		tlsKey:          tlsKey,
	}
}

// InitializeEngine creates and configures the splitting engine
func (ec *EngineController) InitializeEngine(
	sniClient *sni.Client,
	device *sni.Device,
	runConfig *config.RunConfig,
	gameConfig *config.GameConfig,
) error {
	ec.cli.printInfo("Initializing splitting engine...")

	// Create engine configuration
	engineConfig := engine.DefaultEngineConfig()

	// Create the splitting engine
	ec.engine = engine.NewSplittingEngine(
		ec.logger,
		sniClient,
		device,
		runConfig,
		gameConfig,
		engineConfig,
	)

	// Create and initialize LiveSplit server
	ec.liveSplitServer = livesplit.NewServer(ec.logger, "localhost", ec.liveSplitPort, ec.tlsCert, ec.tlsKey)
	ec.liveSplitServer.SetEngine(ec.engine)

	ec.cli.printSuccess("Splitting engine initialized")
	return nil
}

// StartEngine starts the splitting engine
func (ec *EngineController) StartEngine(ctx context.Context) error {
	if ec.engine == nil {
		return fmt.Errorf("engine not initialized")
	}

	ec.cli.printInfo("Starting splitting engine...")

	if err := ec.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}

	// Start monitoring engine events
	go ec.monitorEngineEvents(ctx)

	// Start LiveSplit server if available
	if ec.liveSplitServer != nil {
		ec.cli.printInfo("Starting LiveSplit server...")
		if err := ec.liveSplitServer.Start(ctx); err != nil {
			ec.logger.WithError(err).Error("Failed to start LiveSplit server")
			// Continue even if LiveSplit server fails to start
		} else {
			ec.cli.printSuccess("LiveSplit server started")
		}
	}

	ec.cli.printSuccess("Splitting engine started")
	ec.displayEngineStatus()

	return nil
}

// StopEngine stops the splitting engine
func (ec *EngineController) StopEngine() error {
	if ec.engine == nil {
		return nil
	}

	ec.cli.printInfo("Stopping splitting engine...")

	// Stop LiveSplit server if available
	if ec.liveSplitServer != nil {
		ec.cli.printInfo("Stopping LiveSplit server...")
		if err := ec.liveSplitServer.Stop(context.Background()); err != nil {
			ec.logger.WithError(err).Error("Failed to stop LiveSplit server")
			// Continue even if LiveSplit server fails to stop
		} else {
			ec.cli.printSuccess("LiveSplit server stopped")
		}
	}

	if err := ec.engine.Stop(); err != nil {
		return fmt.Errorf("failed to stop engine: %w", err)
	}

	ec.cli.printSuccess("Splitting engine stopped")
	return nil
}

// RunInteractiveMode runs the interactive engine control mode
func (ec *EngineController) RunInteractiveMode(ctx context.Context) error {
	if ec.engine == nil {
		return fmt.Errorf("engine not initialized")
	}

	ec.cli.printInfo("Entering interactive mode")
	ec.displayCommands()

	for {
		fmt.Print("\nCommand (h for help): ")

		if !ec.cli.scanner.Scan() {
			break
		}

		command := ec.cli.scanner.Text()
		if err := ec.handleCommand(ctx, command); err != nil {
			if err.Error() == "quit" {
				break
			}
			ec.cli.printError(fmt.Sprintf("Command error: %v", err))
		}
	}

	return nil
}

// executeIfManualOpsEnabled executes the given function if manual operations are enabled,
// otherwise prints the provided error message
func (ec *EngineController) executeIfManualOpsEnabled(errorMsg string, fn func() error) error {
	if !ec.enableManualOps {
		ec.cli.printError(errorMsg)
		return nil
	}
	return fn()
}

// handleCommand processes interactive commands
func (ec *EngineController) handleCommand(ctx context.Context, command string) error {
	switch command {
	case "h", "help":
		ec.displayCommands()
	case "s", "status":
		ec.displayEngineStatus()
	case "split":
		return ec.executeIfManualOpsEnabled("Manual split operations are disabled", ec.handleManualSplit)
	case "reset":
		return ec.executeIfManualOpsEnabled("Manual reset operations are disabled", ec.handleManualReset)
	case "pause":
		return ec.executeIfManualOpsEnabled("Manual pause operations are disabled", ec.handlePause)
	case "resume":
		return ec.executeIfManualOpsEnabled("Manual resume operations are disabled", ec.handleResume)
	case "test":
		return ec.executeIfManualOpsEnabled("Manual test condition operations are disabled", ec.handleTestCondition)
	case "stats":
		ec.displayDetailedStats()
	case "q", "quit":
		return fmt.Errorf("quit")
	case "":
		// Empty command, just continue
		return nil
	default:
		ec.cli.printError(fmt.Sprintf("Unknown command: %s (type 'h' for help)", command))
	}
	return nil
}

// displayCommands shows available interactive commands
func (ec *EngineController) displayCommands() {
	fmt.Println("\nAvailable commands:")
	fmt.Println("  h, help    - Show this help")
	fmt.Println("  s, status  - Show current status")
	fmt.Println("  stats      - Show detailed statistics")
	fmt.Println("  q, quit    - Exit interactive mode")

	if ec.enableManualOps {
		fmt.Println("\nDevelopment/Manual commands:")
		fmt.Println("  split      - Manually trigger a split")
		fmt.Println("  reset      - Reset the run")
		fmt.Println("  pause      - Pause the run")
		fmt.Println("  resume     - Resume the run")
		fmt.Println("  test       - Test current split condition")
	}
}

// displayEngineStatus shows the current engine status
func (ec *EngineController) displayEngineStatus() {
	stats := ec.engine.GetStats()

	fmt.Println("\n" + strings.Repeat("─", 60))
	fmt.Printf("Engine Status: %s\n", stats.State.String())
	fmt.Printf("Run: %s (%s)\n", stats.RunName, stats.GameName)

	if stats.State == engine.StateRunning || stats.State == engine.StatePaused {
		fmt.Printf("Progress: %d/%d splits (%.1f%%)\n", stats.CurrentSplit, stats.TotalSplits, stats.Progress)
		if stats.CurrentSplitName != "" {
			fmt.Printf("Current Split: %s\n", stats.CurrentSplitName)
		}
		fmt.Printf("Elapsed Time: %v\n", stats.ElapsedTime.Truncate(time.Millisecond))
		if stats.TimeSinceLastSplit > 0 {
			fmt.Printf("Time Since Last Split: %v\n", stats.TimeSinceLastSplit.Truncate(time.Millisecond))
		}
	}
	fmt.Println(strings.Repeat("─", 60))
}

// displayDetailedStats shows detailed engine statistics
func (ec *EngineController) displayDetailedStats() {
	stats := ec.engine.GetStats()
	session := ec.engine.GetSession()

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("DETAILED STATISTICS")
	fmt.Println(strings.Repeat("=", 60))

	// Basic info
	fmt.Printf("Run Configuration: %s\n", stats.RunName)
	fmt.Printf("Game: %s\n", stats.GameName)
	fmt.Printf("Engine State: %s\n", stats.State.String())
	fmt.Printf("Engine Running: %v\n", stats.IsRunning)
	fmt.Printf("Total Splits: %d\n", stats.TotalSplits)

	// Progress info
	if stats.State == engine.StateRunning || stats.State == engine.StatePaused || stats.State == engine.StateCompleted {
		fmt.Printf("\nProgress Information:\n")
		fmt.Printf("  Current Split: %d/%d\n", stats.CurrentSplit, stats.TotalSplits)
		fmt.Printf("  Progress: %.1f%%\n", stats.Progress)
		if stats.CurrentSplitName != "" {
			fmt.Printf("  Current Split Name: %s\n", stats.CurrentSplitName)
		}

		// Timing info
		fmt.Printf("\nTiming Information:\n")
		fmt.Printf("  Elapsed Time: %v\n", stats.ElapsedTime.Truncate(time.Millisecond))
		if stats.TimeSinceLastSplit > 0 {
			fmt.Printf("  Time Since Last Split: %v\n", stats.TimeSinceLastSplit.Truncate(time.Millisecond))
		}

		// Split states
		fmt.Printf("\nSplit States:\n")
		for i, splitName := range session.GetRunConfig().Splits {
			splitState := session.GetSplitState(splitName)
			status := "Pending"
			if splitState.IsCompleted() {
				status = "Completed"
			} else if i == stats.CurrentSplit && splitState.GetNextStep() > 0 {
				status = fmt.Sprintf("In Progress (Step %d)", splitState.GetNextStep())
			} else if i == stats.CurrentSplit {
				status = "Current"
			}
			fmt.Printf("  %2d. %-20s %s\n", i+1, splitName, status)
		}
	}

	// Autostart info
	fmt.Printf("\nAutostart Configuration:\n")
	autostart := session.GetGameConfig().Autostart
	fmt.Printf("  Address: %s\n", autostart.Address)
	fmt.Printf("  Value: %s\n", autostart.Value)
	fmt.Printf("  Type: %s\n", autostart.Type)
	if autostart.Note != "" {
		fmt.Printf("  Note: %s\n", autostart.Note)
	}

	fmt.Println(strings.Repeat("=", 60))
}

// handleManualSplit handles manual split command
func (ec *EngineController) handleManualSplit() error {
	if err := ec.engine.ManualSplit(); err != nil {
		return fmt.Errorf("failed to split: %w", err)
	}
	ec.cli.printSuccess("Split triggered manually")
	return nil
}

// handleManualReset handles manual reset command
func (ec *EngineController) handleManualReset() error {
	if err := ec.engine.ManualReset(); err != nil {
		return fmt.Errorf("failed to reset: %w", err)
	}
	ec.cli.printSuccess("Run reset")
	return nil
}

// handlePause handles pause command
func (ec *EngineController) handlePause() error {
	if err := ec.engine.PauseEngine(); err != nil {
		return fmt.Errorf("failed to pause: %w", err)
	}
	ec.cli.printSuccess("Run paused")
	return nil
}

// handleResume handles resume command
func (ec *EngineController) handleResume() error {
	if err := ec.engine.ResumeEngine(); err != nil {
		return fmt.Errorf("failed to resume: %w", err)
	}
	ec.cli.printSuccess("Run resumed")
	return nil
}

// handleTestCondition tests the current split condition
func (ec *EngineController) handleTestCondition() error {
	split, splitState, err := ec.engine.GetCurrentSplitCondition()
	if err != nil {
		return fmt.Errorf("failed to get current split condition: %w", err)
	}

	ec.cli.printInfo(fmt.Sprintf("Testing condition for split: %s", split.Name))

	result, err := ec.engine.TestCondition(split)
	if err != nil {
		return fmt.Errorf("failed to test condition: %w", err)
	}

	if result {
		ec.cli.printSuccess("✓ Condition is currently TRUE")
	} else {
		ec.cli.printInfo("✗ Condition is currently FALSE")
	}

	// Show condition details
	fmt.Printf("Condition Details:\n")
	fmt.Printf("  Address: %s (0x%X)\n", split.Address, split.GetFxPakProAddress())
	fmt.Printf("  Value: %s (0x%X)\n", split.Value, split.ValueInt())
	fmt.Printf("  Type: %s\n", split.Type)

	if len(split.More) > 0 {
		fmt.Printf("  Additional Conditions (More): %d\n", len(split.More))
	}

	if len(split.Next) > 0 {
		fmt.Printf("  Sequential Conditions (Next): %d\n", len(split.Next))
		fmt.Printf("  Current Step: %d\n", splitState.GetNextStep())
	}

	return nil
}

// monitorEngineEvents monitors engine events and provides UI feedback
func (ec *EngineController) monitorEngineEvents(ctx context.Context) {
	splitChan := ec.engine.RegisterSplitChannel(ctx)
	statusChan := ec.engine.RegisterStatusChannel(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case splitEvent, ok := <-splitChan:
			if !ok {
				return
			}
			ec.handleSplitEvent(splitEvent)
		case statusEvent, ok := <-statusChan:
			if !ok {
				return
			}
			ec.handleStatusEvent(statusEvent)
		}
	}
}

// handleSplitEvent handles split events from the engine
func (ec *EngineController) handleSplitEvent(event engine.SplitEvent) {
	switch event.Action {
	case engine.SplitActionSplit:
		ec.cli.printSuccess(fmt.Sprintf("🚀 SPLIT: %s (Split %d)", event.SplitName, event.SplitIndex+1))

		// Show progress for completed splits
		stats := ec.engine.GetStats()
		if stats.TotalSplits > 0 {
			fmt.Printf("Progress: %d/%d splits (%.1f%%) completed\n",
				event.SplitIndex+1, stats.TotalSplits, float64(event.SplitIndex+1)/float64(stats.TotalSplits)*100)
		}

	case engine.SplitActionSkip:
		ec.cli.printWarning(fmt.Sprintf("⏭️  SKIP: %s (Split %d)", event.SplitName, event.SplitIndex+1))

	case engine.SplitActionUndo:
		ec.cli.printInfo(fmt.Sprintf("↩️  UNDO: %s (Split %d)", event.SplitName, event.SplitIndex+1))

	default:
		ec.cli.printInfo(fmt.Sprintf("❓ %s: %s (Split %d)", event.Action.String(), event.SplitName, event.SplitIndex+1))
	}
}

// handleStatusEvent handles status events from the engine
func (ec *EngineController) handleStatusEvent(event engine.StatusEvent) {
	switch event.State {
	case engine.StateWaitingForStart:
		ec.cli.printInfo("⏳ " + event.Message)
		// Show special autostart message
		ec.cli.printInfo("Autostart enabled - waiting for start condition...")
		ec.cli.printInfo("The run will automatically start when the game begins")
	case engine.StateRunning:
		ec.cli.printSuccess("▶️ " + event.Message)
		// Show special game start message if transitioning from waiting
		if event.Message == "Run started" {
			ec.cli.printSuccess("🎮 Game started - run is now active!")
		}
	case engine.StatePaused:
		ec.cli.printWarning("⏸️ " + event.Message)
	case engine.StateCompleted:
		ec.cli.printSuccess("🏁 " + event.Message)
		ec.cli.printSuccess("🎉 RUN COMPLETED!")
		ec.cli.printInfo("Reset using LiveSplit to start a new run")
	case engine.StateError:
		ec.cli.printError("❌ " + event.Message)
	default:
		ec.cli.printInfo("ℹ️ " + event.Message)
	}
}

// GetEngine returns the underlying splitting engine (for external access)
func (ec *EngineController) GetEngine() *engine.SplittingEngine {
	return ec.engine
}

// IsInitialized returns whether the engine has been initialized
func (ec *EngineController) IsInitialized() bool {
	return ec.engine != nil
}

// ValidateConfiguration validates the engine configuration before initialization
func (ec *EngineController) ValidateConfiguration(runConfig *config.RunConfig, gameConfig *config.GameConfig) error {
	// Validate that all splits in the run exist in the game config
	for i, splitName := range runConfig.Splits {
		_, err := gameConfig.GetSplitByName(splitName)
		if err != nil {
			return fmt.Errorf("split '%s' at index %d not found in game config: %w", splitName, i, err)
		}
	}

	// Validate autostart configuration
	if gameConfig.Autostart.Address == "" {
		return fmt.Errorf("autostart is enabled but missing address")
	}
	if gameConfig.Autostart.Value == "" {
		return fmt.Errorf("autostart is enabled but missing value")
	}
	if gameConfig.Autostart.Type == "" {
		return fmt.Errorf("autostart is enabled but missing type")
	}

	return nil
}

package ui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/jdharms/sni-autosplitter/internal/config"
	"github.com/jdharms/sni-autosplitter/internal/sni"
	sniproto "github.com/jdharms/sni-autosplitter/pkg/sni"
	"github.com/sirupsen/logrus"
)

// CLI handles the command-line interface for the autosplitter
type CLI struct {
	logger          *logrus.Logger
	configLoader    *config.ConfigLoader
	scanner         *bufio.Scanner
	enableManualOps bool
}

// NewCLI creates a new CLI interface
func NewCLI(logger *logrus.Logger, gamesDir, runsDir string, enableManualOps bool) *CLI {
	return &CLI{
		logger:          logger,
		configLoader:    config.NewConfigLoader(logger, gamesDir, runsDir),
		scanner:         bufio.NewScanner(os.Stdin),
		enableManualOps: enableManualOps,
	}
}

// Start initializes and starts the autosplitter with the given run name
func (c *CLI) Start(runName string) error {
	c.printHeader()

	// Discover all available runs
	c.printInfo("Discovering run configurations...")
	runs, err := c.configLoader.DiscoverRuns()
	if err != nil {
		return fmt.Errorf("failed to discover runs: %w", err)
	}

	if len(runs) == 0 {
		return fmt.Errorf("no valid run configurations found")
	}

	c.printSuccess(fmt.Sprintf("Found %d run configuration(s)", len(runs)))

	// Select run configuration
	var selectedRun *config.RunConfig
	if runName != "" {
		// Find run by specified name/category
		selectedRun, err = c.configLoader.FindRunByCategory(runs, runName)
		if err != nil {
			c.printError(fmt.Sprintf("Run selection failed: %v", err))
			c.printInfo("Available runs:")
			c.listRuns(runs)
			return err
		}
		c.printSuccess(fmt.Sprintf("Selected run: %s", selectedRun.Category))
	} else {
		// Interactive run selection
		selectedRun, err = c.selectRunInteractive(runs)
		if err != nil {
			return fmt.Errorf("run selection failed: %w", err)
		}
	}

	// Load and validate run and game configurations
	c.printInfo("Loading game configuration...")
	runConfig, gameConfig, err := c.configLoader.LoadRunAndGame(selectedRun)
	if err != nil {
		return fmt.Errorf("failed to load configurations: %w", err)
	}

	c.printSuccess(fmt.Sprintf("Loaded: %s - %s (%d splits)", gameConfig.Name, runConfig.Category, len(runConfig.Splits)))

	// Phase 2: SNI Integration
	ctx := context.Background()

	// Initialize SNI client
	c.printInfo("Connecting to SNI server...")
	sniClient := sni.NewClient(c.logger, "localhost", 8191)

	// Connect to SNI with retry logic
	err = sniClient.ConnectWithRetry(ctx, 3)
	if err != nil {
		c.printError(fmt.Sprintf("Failed to connect to SNI: %v", err))
		c.printInfo("Make sure SNI is running and accessible at localhost:8191")
		return err
	}
	defer sniClient.Disconnect()

	c.printSuccess("Connected to SNI server")

	// Initialize device manager and selector
	deviceManager := sni.NewDeviceManager(c.logger, sniClient)
	deviceSelector := NewDeviceSelector(c.logger, deviceManager, c)

	// Select SNI device
	selectedDevice, err := deviceSelector.SelectDevice(ctx)
	if err != nil {
		return fmt.Errorf("device selection failed: %w", err)
	}

	// Detect memory mapping
	c.printInfo("Detecting memory mapping...")
	memoryMapping, err := deviceManager.DetectMemoryMapping(ctx, selectedDevice)
	if err != nil {
		c.printWarning(fmt.Sprintf("Memory mapping detection failed, using LoROM: %v", err))
		memoryMapping = sniproto.MemoryMapping_LoROM
	}

	mappingName := getMemoryMappingName(memoryMapping)
	c.printSuccess(fmt.Sprintf("Memory mapping: %s", mappingName))

	c.printSuccess("SNI integration complete!")
	c.printInfo(fmt.Sprintf("Ready to autosplit: %s - %s", gameConfig.Name, runConfig.Category))
	c.printInfo(fmt.Sprintf("Device: %s", selectedDevice.DisplayName))
	c.printInfo(fmt.Sprintf("Memory mapping: %s", mappingName))

	// Phase 3: Initialize and start splitting engine
	c.printInfo("Initializing splitting engine...")

	engineController := NewEngineController(c.logger, c, c.enableManualOps)

	// Validate configuration before initializing engine
	if err := engineController.ValidateConfiguration(runConfig, gameConfig); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Initialize the splitting engine
	if err := engineController.InitializeEngine(sniClient, selectedDevice, runConfig, gameConfig); err != nil {
		return fmt.Errorf("failed to initialize splitting engine: %w", err)
	}

	// Start the splitting engine
	if err := engineController.StartEngine(ctx); err != nil {
		return fmt.Errorf("failed to start splitting engine: %w", err)
	}
	defer engineController.StopEngine()

	// Enter interactive mode
	c.printInfo("Entering interactive mode...")
	c.printInfo("You can now control the autosplitter or let it run automatically")

	if err := engineController.RunInteractiveMode(ctx); err != nil {
		c.printWarning(fmt.Sprintf("Interactive mode ended: %v", err))
	}

	return nil
}

// selectRunInteractive presents an interactive menu for run selection
func (c *CLI) selectRunInteractive(runs []*config.RunConfig) (*config.RunConfig, error) {
	c.printInfo("Available runs:")
	c.listRuns(runs)

	for {
		fmt.Print("\nSelect run (1-" + strconv.Itoa(len(runs)) + ") or 'q' to quit: ")

		if !c.scanner.Scan() {
			return nil, fmt.Errorf("failed to read input")
		}

		input := strings.TrimSpace(c.scanner.Text())

		if input == "q" || input == "quit" {
			return nil, fmt.Errorf("user quit")
		}

		choice, err := strconv.Atoi(input)
		if err != nil || choice < 1 || choice > len(runs) {
			c.printError(fmt.Sprintf("Invalid selection. Please enter a number between 1 and %d, or 'q' to quit.", len(runs)))
			continue
		}

		selectedRun := runs[choice-1]
		c.printSuccess(fmt.Sprintf("Selected: %s", selectedRun.Category))
		return selectedRun, nil
	}
}

// listRuns displays a numbered list of available runs
func (c *CLI) listRuns(runs []*config.RunConfig) {
	for i, run := range runs {
		fmt.Printf("  %d. %s (%s - %d splits)\n", i+1, run.Name, run.Game, len(run.Splits))
	}
}

// printHeader displays the application header
func (c *CLI) printHeader() {
	header := color.New(color.FgCyan, color.Bold)
	header.Println("┌─────────────────────────────────────────────────────────────┐")
	header.Println("│                    SNI AutoSplitter                         │")
	header.Println("│          LiveSplit One + Super Nintendo Interface           │")
	header.Println("└─────────────────────────────────────────────────────────────┘")
	fmt.Println()
}

// printInfo prints an informational message
func (c *CLI) printInfo(message string) {
	info := color.New(color.FgBlue)
	info.Printf("[INFO] %s\n", message)
}

// printSuccess prints a success message
func (c *CLI) printSuccess(message string) {
	success := color.New(color.FgGreen)
	success.Printf("[SUCCESS] %s\n", message)
}

// printError prints an error message
func (c *CLI) printError(message string) {
	errorColor := color.New(color.FgRed)
	errorColor.Printf("[ERROR] %s\n", message)
}

// printWarning prints a warning message
func (c *CLI) printWarning(message string) {
	warning := color.New(color.FgYellow)
	warning.Printf("[WARNING] %s\n", message)
}

// getMemoryMappingName returns a human-readable name for memory mapping
func getMemoryMappingName(mapping sniproto.MemoryMapping) string {
	switch mapping {
	case sniproto.MemoryMapping_LoROM:
		return "LoROM"
	case sniproto.MemoryMapping_HiROM:
		return "HiROM"
	case sniproto.MemoryMapping_ExHiROM:
		return "ExHiROM"
	case sniproto.MemoryMapping_SA1:
		return "SA1"
	case sniproto.MemoryMapping_Unknown:
		return "Unknown"
	default:
		return fmt.Sprintf("Unknown (%d)", mapping)
	}
}

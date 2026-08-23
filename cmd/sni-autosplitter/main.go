package main

import (
	"fmt"
	"os"
	"time"
	"strings"

	"github.com/jdharms/sni-autosplitter/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	// MemoryPollInterval is the interval between memory reads in milliseconds
	MemoryPollInterval = time.Second / 60 // 60 FPS
)

var (
	version = "dev"

	// Command line flags
	runName         string
	gamesDir        string
	runsDir         string
	logDir          string
	logLevel        string
	sniHost         string
	sniPort         int
	liveSplitPort   int
	enableManualOps bool
	tlsCert         string
	tlsKey          string

	rootCmd = &cobra.Command{
		Use:     "sni-autosplitter",
		Version: version,
		Short:   "SNI-based autosplitter for LiveSplit One",
		Long:    `SNI AutoSplitter connects to SNI (Super Nintendo Interface) to read game memory
and automatically trigger splits in LiveSplit One based on configurable conditions.

The autosplitter uses a two-tier configuration system:
- Game configs define memory addresses and conditions
- Run configs define split sequences for specific categories

Examples:
  sni-autosplitter                              # Interactive mode - select from available runs
  sni-autosplitter --run-name "alttp any% nmg"  # Load specific run by name
  sni-autosplitter --games-dir ./custom/games --runs-dir ./custom/runs`,
		Args: cobra.NoArgs,
		Run:  runAutosplitter,
	}
)

func init() {
	// Override flag variables from env/flags before running
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		runName = viper.GetString("run-name")
		gamesDir = viper.GetString("games-dir")
		runsDir = viper.GetString("runs-dir")
		logDir = viper.GetString("log-dir")
		logLevel = viper.GetString("log-level")
		sniHost = viper.GetString("sni-host")
		sniPort = viper.GetInt("sni-port")
		liveSplitPort = viper.GetInt("livesplit-port")
		enableManualOps = viper.GetBool("enable-manual-ops")
		tlsCert = viper.GetString("tls-cert")
		tlsKey = viper.GetString("tls-key")
	}

	// Set up command line flags
	rootCmd.PersistentFlags().StringVar(&runName, "run-name", "", "Name of the run to load")
	rootCmd.PersistentFlags().StringVar(&gamesDir, "games-dir", "./configs/games", "Directory containing game configuration files")
	rootCmd.PersistentFlags().StringVar(&runsDir, "runs-dir", "./configs/runs", "Directory containing run configuration files")
	rootCmd.PersistentFlags().StringVar(&logDir, "log-dir", "./logs", "Directory for log files")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&sniHost, "sni-host", "localhost", "SNI gRPC server host")
	rootCmd.PersistentFlags().IntVar(&sniPort, "sni-port", 8191, "SNI gRPC server port")
	rootCmd.PersistentFlags().IntVar(&liveSplitPort, "livesplit-port", 1990, "WebSocket port for LiveSplit One connections")
	rootCmd.PersistentFlags().BoolVar(&enableManualOps, "enable-manual-ops", false, "Enable manual split operations for development (split, reset, pause, resume, test)")
	rootCmd.PersistentFlags().StringVar(&tlsCert, "tls-cert", "", "Path to TLS certificate file (enables WSS)")
	rootCmd.PersistentFlags().StringVar(&tlsKey, "tls-key", "", "Path to TLS private key file (enables WSS)")

	// Environment variable support and bind all flags
	viper.SetEnvPrefix("sni_autosplitter")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.BindPFlags(rootCmd.PersistentFlags())
}

func runAutosplitter(cmd *cobra.Command, args []string) {
	// Initialize logger first
	logger, err := ui.InitializeLogger(logDir, logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	logger.Info("SNI AutoSplitter starting up")
	logger.WithFields(map[string]any{
		"run-name":          runName,
		"games-dir":         gamesDir,
		"runs-dir":          runsDir,
		"log-dir":           logDir,
		"log-level":         logLevel,
		"sni-host":          sniHost,
		"sni-port":          sniPort,
		"livesplit-port":    liveSplitPort,
		"enable-manual-ops": enableManualOps,
	}).Info("Configuration loaded")

	// Create and start the CLI interface
	cliInterface := ui.NewCLI(logger, gamesDir, runsDir, enableManualOps, sniHost, sniPort, liveSplitPort, tlsCert, tlsKey)

	if err := cliInterface.Start(runName); err != nil {
		logger.WithError(err).Fatal("Failed to start autosplitter")
		os.Exit(1)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

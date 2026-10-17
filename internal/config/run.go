package config

import (
	"fmt"
)

// RunConfig represents a run configuration that defines a sequence of splits
type RunConfig struct {
	Name             string   `json:"name"`
	Category         string   `json:"category"`
	Game             string   `json:"game"`
	Splits           []string `json:"splits"`
	DisableAutostart bool     `json:"disableAutostart,omitempty"`
}

// Validate validates the run configuration
func (rc *RunConfig) Validate() error {
	if rc.Name == "" {
		return fmt.Errorf("run config missing required 'name' field")
	}

	if rc.Category == "" {
		return fmt.Errorf("run config '%s' missing required 'category' field", rc.Name)
	}

	if rc.Game == "" {
		return fmt.Errorf("run config '%s' missing required 'game' field", rc.Name)
	}

	if len(rc.Splits) == 0 {
		return fmt.Errorf("run config '%s' must have at least one split", rc.Name)
	}

	// Check for duplicate split names
	splitNames := make(map[string]bool)
	for i, splitName := range rc.Splits {
		if splitName == "" {
			return fmt.Errorf("run config '%s' has empty split name at index %d", rc.Name, i)
		}

		if splitNames[splitName] {
			return fmt.Errorf("run config '%s' has duplicate split name '%s'", rc.Name, splitName)
		}
		splitNames[splitName] = true
	}

	return nil
}

// ValidateAgainstGame validates that all splits in the run exist in the game config
func (rc *RunConfig) ValidateAgainstGame(gameConfig *GameConfig) error {
	for i, splitName := range rc.Splits {
		found := false

		// Check if split exists in game definitions
		for _, gameSplit := range gameConfig.Definitions {
			if gameSplit.Name == splitName {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("run config '%s' references split '%s' at index %d, but it doesn't exist in game config '%s'",
				rc.Name, splitName, i, gameConfig.Name)
		}
	}

	return nil
}

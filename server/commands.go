package server

import (
	"fmt"

	"github.com/spf13/cobra"

	rollconf "github.com/rollkit/rollkit/pkg/config"
)

// InitCmd is meant to be used for initializing the rollkit config.
func InitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize rollkit configuration files.",
		Args:  cobra.NoArgs,
		RunE:  InitRunE,
	}

	rollconf.AddFlags(cmd)

	return cmd
}

// InitRunE is the function that is called when the init command is run.
// It can be used standalone to wrap the SDK genesis init command.
// Or used via the InitCmd function to create a new command.
func InitRunE(cmd *cobra.Command, args []string) error {
	aggregator, err := cmd.Flags().GetBool(rollconf.FlagAggregator)
	if err != nil {
		return fmt.Errorf("error reading aggregator flag: %w", err)
	}

	// ignore error, as we are creating a new config
	// we use load in order to parse all the flags
	cfg, _ := rollconf.Load(cmd)
	cfg.Node.Aggregator = aggregator

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("error validating config: %w", err)
	}

	if err := cfg.SaveAsYaml(); err != nil {
		return fmt.Errorf("error writing rollkit.yaml file: %w", err)
	}

	return nil
}

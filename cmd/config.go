package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/lothardp/hive/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage repo configuration",
}

var configFile string

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current effective config",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if app.Config == nil {
			return fmt.Errorf("no config available — not in a git repo or repo not registered")
		}

		data, err := app.Config.ToYAML()
		if err != nil {
			return fmt.Errorf("marshaling config: %w", err)
		}

		source := "file (.hive.yaml)"
		if app.RepoRecord != nil {
			source = "database"
		}
		fmt.Printf("# Source: %s\n", source)
		fmt.Print(string(data))
		return nil
	},
}

var configExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export repo config to YAML",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if app.RepoRecord == nil {
			return fmt.Errorf("repo not registered — run 'hive setup' first")
		}

		data, err := app.Config.ToYAML()
		if err != nil {
			return fmt.Errorf("marshaling config: %w", err)
		}

		if configFile != "" {
			if err := os.WriteFile(configFile, data, 0o644); err != nil {
				return fmt.Errorf("writing file: %w", err)
			}
			fmt.Printf("Config exported to %s\n", configFile)
			return nil
		}

		fmt.Print(string(data))
		return nil
	},
}

var configImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import repo config from YAML",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if app.RepoRecord == nil {
			return fmt.Errorf("repo not registered — run 'hive setup' first")
		}

		var data []byte
		var err error
		if configFile != "" {
			data, err = os.ReadFile(configFile)
			if err != nil {
				return fmt.Errorf("reading file: %w", err)
			}
		} else {
			data, err = io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
		}

		var cfg config.ProjectConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing YAML: %w", err)
		}

		jsonStr, err := cfg.ToJSON()
		if err != nil {
			return fmt.Errorf("serializing config: %w", err)
		}

		app.RepoRecord.Config = jsonStr
		if err := app.RepoRepo.Update(ctx, app.RepoRecord); err != nil {
			return fmt.Errorf("updating repo config: %w", err)
		}

		fmt.Println("Config imported successfully")
		return nil
	},
}

func init() {
	configExportCmd.Flags().StringVarP(&configFile, "file", "f", "", "Output file path (default: stdout)")
	configImportCmd.Flags().StringVarP(&configFile, "file", "f", "", "Input file path (default: stdin)")

	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configExportCmd)
	configCmd.AddCommand(configImportCmd)
	rootCmd.AddCommand(configCmd)
}

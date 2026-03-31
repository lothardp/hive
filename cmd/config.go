package cmd

import (
	"encoding/json"
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

var configApplyGlobal bool

var configApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Merge config from YAML into current repo config",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

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

		var incoming config.ProjectConfig
		if err := yaml.Unmarshal(data, &incoming); err != nil {
			return fmt.Errorf("parsing YAML: %w", err)
		}

		if configApplyGlobal {
			// Store layouts in global_config
			if incoming.Layouts == nil || len(incoming.Layouts) == 0 {
				return fmt.Errorf("no layouts found in YAML — only layouts are supported at the global level")
			}
			existing, err := app.ConfigRepo.Get(ctx, "layouts")
			if err != nil {
				return fmt.Errorf("reading global layouts: %w", err)
			}
			var globalLayouts map[string]config.Layout
			if existing != "" {
				if err := json.Unmarshal([]byte(existing), &globalLayouts); err != nil {
					return fmt.Errorf("parsing existing global layouts: %w", err)
				}
			} else {
				globalLayouts = make(map[string]config.Layout)
			}
			for k, v := range incoming.Layouts {
				globalLayouts[k] = v
			}
			encoded, err := json.Marshal(globalLayouts)
			if err != nil {
				return fmt.Errorf("serializing global layouts: %w", err)
			}
			if err := app.ConfigRepo.Set(ctx, "layouts", string(encoded)); err != nil {
				return fmt.Errorf("saving global layouts: %w", err)
			}
			fmt.Printf("Applied %d layout(s) to global config\n", len(incoming.Layouts))
			return nil
		}

		// Repo-level apply
		if app.RepoRecord == nil {
			return fmt.Errorf("repo not registered — run 'hive setup' first")
		}

		existing, err := config.ProjectConfigFromJSON(app.RepoRecord.Config)
		if err != nil {
			return fmt.Errorf("parsing existing config: %w", err)
		}

		existing.Merge(&incoming)

		jsonStr, err := existing.ToJSON()
		if err != nil {
			return fmt.Errorf("serializing config: %w", err)
		}

		app.RepoRecord.Config = jsonStr
		if err := app.RepoRepo.Update(ctx, app.RepoRecord); err != nil {
			return fmt.Errorf("updating repo config: %w", err)
		}

		fmt.Println("Config applied successfully")
		return nil
	},
}

func init() {
	configExportCmd.Flags().StringVarP(&configFile, "file", "f", "", "Output file path (default: stdout)")
	configImportCmd.Flags().StringVarP(&configFile, "file", "f", "", "Input file path (default: stdin)")
	configApplyCmd.Flags().StringVarP(&configFile, "file", "f", "", "Input file path (default: stdin)")
	configApplyCmd.Flags().BoolVar(&configApplyGlobal, "global", false, "Apply layouts to global config instead of repo")

	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configExportCmd)
	configCmd.AddCommand(configImportCmd)
	configCmd.AddCommand(configApplyCmd)
	rootCmd.AddCommand(configCmd)
}

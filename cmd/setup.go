package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/shell"
	"github.com/lothardp/hive/internal/state"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Register the current repo with Hive",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if app.RepoDir == "" {
			return fmt.Errorf("not in a git repository — run this from inside a project")
		}

		reader := bufio.NewReader(os.Stdin)
		var updating bool

		// Check if already registered
		existing, _ := app.RepoRepo.GetByPath(ctx, app.RepoDir)
		if existing != nil {
			fmt.Printf("Repo %q is already registered.\n", existing.Name)
			fmt.Print("Update config? [y/N] ")
			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(answer)) != "y" {
				return nil
			}
			updating = true
		}

		// Detect project name
		projectName := app.Project
		fmt.Printf("Project name [%s]: ", projectName)
		input, _ := reader.ReadString('\n')
		if s := strings.TrimSpace(input); s != "" {
			projectName = s
		}

		// Detect remote URL
		remoteURL := ""
		res, err := shell.RunInDir(ctx, app.RepoDir, "git", "config", "--get", "remote.origin.url")
		if err == nil && res.ExitCode == 0 {
			remoteURL = strings.TrimSpace(res.Stdout)
		}
		fmt.Printf("Remote URL [%s]: ", remoteURL)
		input, _ = reader.ReadString('\n')
		if s := strings.TrimSpace(input); s != "" {
			remoteURL = s
		}

		// Detect default branch
		defaultBranch := "main"
		res, err = shell.RunInDir(ctx, app.RepoDir, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
		if err == nil && res.ExitCode == 0 {
			// refs/remotes/origin/main -> main
			parts := strings.Split(strings.TrimSpace(res.Stdout), "/")
			if len(parts) > 0 {
				defaultBranch = parts[len(parts)-1]
			}
		}
		fmt.Printf("Default branch [%s]: ", defaultBranch)
		input, _ = reader.ReadString('\n')
		if s := strings.TrimSpace(input); s != "" {
			defaultBranch = s
		}

		// Build config — pre-populate from existing .hive.yaml if present
		cfg := config.LoadOrDefault(app.RepoDir)
		if updating {
			// Pre-populate from DB config
			if dbCfg, err := config.ProjectConfigFromJSON(existing.Config); err == nil {
				cfg = dbCfg
			}
		}

		fmt.Printf("\nCompose path [%s]: ", cfg.ComposePath)
		input, _ = reader.ReadString('\n')
		if s := strings.TrimSpace(input); s != "" {
			cfg.ComposePath = s
		}

		fmt.Printf("Expose port [%d]: ", cfg.ExposePort)
		input, _ = reader.ReadString('\n')
		if s := strings.TrimSpace(input); s != "" {
			if port, err := strconv.Atoi(s); err == nil {
				cfg.ExposePort = port
			}
		}

		fmt.Println("Seed scripts (one per line, empty line to finish):")
		if len(cfg.SeedScripts) > 0 {
			fmt.Printf("  Current: %s\n", strings.Join(cfg.SeedScripts, ", "))
		}
		var seeds []string
		for {
			fmt.Print("  > ")
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			seeds = append(seeds, line)
		}
		if len(seeds) > 0 {
			cfg.SeedScripts = seeds
		}

		fmt.Println("Environment variables (KEY=VALUE, empty line to finish):")
		if len(cfg.Env) > 0 {
			for k, v := range cfg.Env {
				fmt.Printf("  Current: %s=%s\n", k, v)
			}
		}
		envVars := make(map[string]string)
		for k, v := range cfg.Env {
			envVars[k] = v
		}
		for {
			fmt.Print("  > ")
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				envVars[parts[0]] = parts[1]
			}
		}
		cfg.Env = envVars

		// Serialize config
		configJSON, err := cfg.ToJSON()
		if err != nil {
			return fmt.Errorf("serializing config: %w", err)
		}

		if updating {
			existing.Name = projectName
			existing.RemoteURL = remoteURL
			existing.DefaultBranch = defaultBranch
			existing.Config = configJSON
			if err := app.RepoRepo.Update(ctx, existing); err != nil {
				return fmt.Errorf("updating repo: %w", err)
			}
		} else {
			repo := &state.Repo{
				Name:          projectName,
				Path:          app.RepoDir,
				RemoteURL:     remoteURL,
				DefaultBranch: defaultBranch,
				Config:        configJSON,
			}
			if err := app.RepoRepo.Create(ctx, repo); err != nil {
				return fmt.Errorf("registering repo: %w", err)
			}
		}

		fmt.Printf("\nRepo %q registered!\n", projectName)
		fmt.Printf("  Path:     %s\n", app.RepoDir)
		fmt.Printf("  Remote:   %s\n", remoteURL)
		fmt.Printf("  Branch:   %s\n", defaultBranch)
		fmt.Printf("  Config:   %s\n", configJSON)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lothardp/hive/internal/state"
	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Show cell consistency across DB, disk, and tmux",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// 1. Collect DB cells
		cells, err := app.CellRepo.List(ctx)
		if err != nil {
			return fmt.Errorf("listing cells from DB: %w", err)
		}
		dbCells := make(map[string]state.Cell, len(cells))
		for _, c := range cells {
			dbCells[c.Name] = c
		}

		// 2. Collect clone directories on disk
		// Structure: <cells_dir>/<project>/<name> → cell name is <project>-<name>
		diskCells := make(map[string]string) // cell name → path
		cellsDir := app.Config.ResolveCellsDir()
		projects, err := os.ReadDir(cellsDir)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reading cells directory: %w", err)
		}
		for _, proj := range projects {
			if !proj.IsDir() {
				continue
			}
			projPath := filepath.Join(cellsDir, proj.Name())
			entries, err := os.ReadDir(projPath)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				cellName := proj.Name() + "-" + entry.Name()
				diskCells[cellName] = filepath.Join(projPath, entry.Name())
			}
		}

		// 2b. Collect multicell parent directories on disk.
		// Structure: <multicells_dir>/<name> → cell name is just <name>
		multicellsDir := app.Config.ResolveMulticellsDir()
		mcDirs, err := os.ReadDir(multicellsDir)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reading multicells directory: %w", err)
		}
		for _, entry := range mcDirs {
			if !entry.IsDir() {
				continue
			}
			diskCells[entry.Name()] = filepath.Join(multicellsDir, entry.Name())
		}

		// 3. Collect tmux sessions
		tmuxSessions, err := app.TmuxMgr.ListSessions(ctx)
		if err != nil {
			return fmt.Errorf("listing tmux sessions: %w", err)
		}
		tmuxSet := make(map[string]bool, len(tmuxSessions))
		for _, s := range tmuxSessions {
			if s == "hive" {
				continue // dashboard session, not a cell
			}
			tmuxSet[s] = true
		}

		// 4. Build union of all cell names
		allNames := make(map[string]bool)
		for name := range dbCells {
			allNames[name] = true
		}
		for name := range diskCells {
			allNames[name] = true
		}
		for name := range tmuxSet {
			allNames[name] = true
		}

		sorted := make([]string, 0, len(allNames))
		for name := range allNames {
			sorted = append(sorted, name)
		}
		sort.Strings(sorted)

		// 5. Print report
		fmt.Printf("%-35s  %-6s  %-6s  %-6s  %s\n", "CELL", "DB", "DISK", "TMUX", "STATUS")
		fmt.Printf("%s\n", strings.Repeat("─", 80))

		healthy := 0
		issues := 0

		for _, name := range sorted {
			_, inDB := dbCells[name]
			_, onDisk := diskCells[name]
			inTmux := tmuxSet[name]

			dbCol := mark(inDB)
			diskCol := mark(onDisk)
			tmuxCol := mark(inTmux)

			// Determine status
			status := ""
			cellType := state.TypeNormal
			if inDB {
				cellType = dbCells[name].Type
			}
			isHeadless := cellType == state.TypeHeadless
			isMulti := cellType == state.TypeMulti

			if isHeadless {
				// Headless cells: only need DB + tmux, no disk
				if inDB && inTmux {
					status = "ok"
					healthy++
				} else {
					status = describeHeadlessIssue(inDB, inTmux)
					issues++
				}
				diskCol = "  -   " // not applicable
			} else if isMulti {
				// Multicells: need DB + parent dir + tmux
				if inDB && onDisk && inTmux {
					status = "ok (multi)"
					healthy++
				} else {
					status = "multi: " + describeIssue(inDB, onDisk, inTmux)
					issues++
				}
			} else if inDB && onDisk && inTmux {
				status = "ok"
				healthy++
			} else if !inDB && !onDisk && inTmux {
				status = "unmanaged tmux session"
				issues++
			} else {
				status = describeIssue(inDB, onDisk, inTmux)
				issues++
			}

			fmt.Printf("%-35s  %-6s  %-6s  %-6s  %s\n", name, dbCol, diskCol, tmuxCol, status)
		}

		fmt.Printf("%s\n", strings.Repeat("─", 80))
		fmt.Printf("%d healthy, %d issues\n", healthy, issues)

		return nil
	},
}

func mark(ok bool) string {
	if ok {
		return "  ✓   "
	}
	return "  ✗   "
}

func describeIssue(inDB, onDisk, inTmux bool) string {
	var missing []string
	if !inDB {
		missing = append(missing, "no DB record")
	}
	if !onDisk {
		missing = append(missing, "no clone dir")
	}
	if !inTmux {
		missing = append(missing, "no tmux session")
	}
	return strings.Join(missing, ", ")
}

func describeHeadlessIssue(inDB, inTmux bool) string {
	var missing []string
	if !inDB {
		missing = append(missing, "no DB record")
	}
	if !inTmux {
		missing = append(missing, "no tmux session")
	}
	return strings.Join(missing, ", ")
}

func init() {
	rootCmd.AddCommand(healthCmd)
}

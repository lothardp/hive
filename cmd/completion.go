package cmd

import "github.com/spf13/cobra"

// completeCellNames provides shell completion for commands that take a cell name argument.
func completeCellNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if app.Repo == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cells, err := app.Repo.List(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var names []string
	for _, c := range cells {
		names = append(names, c.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

package cli

import (
	"os"

	"github.com/spf13/cobra"
)

func completionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for fiatlux.

Bash:
  fiatlux completion bash > /etc/bash_completion.d/fiatlux
  # or for the current user:
  fiatlux completion bash > ~/.local/share/bash-completion/completions/fiatlux

Zsh:
  fiatlux completion zsh > "${fpath[1]}/_fiatlux"
  # then restart your shell or run: autoload -U compinit && compinit

Fish:
  fiatlux completion fish > ~/.config/fish/completions/fiatlux.fish

PowerShell:
  fiatlux completion powershell | Out-String | Invoke-Expression
  # to persist, add that line to your $PROFILE`,
		ValidArgs:    []string{"bash", "zsh", "fish", "powershell"},
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return cmd.Help()
			}
		},
	}
	return cmd
}

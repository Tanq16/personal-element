package clientCmd

import (
	"errors"

	"github.com/spf13/cobra"

	u "element-agent/utils"
)

var setupFlags struct {
	serverURL string
	secret    string
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Store the server URL and the shared secret",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		secret := setupFlags.secret
		if secret == "" {
			entered, err := u.PromptPassword("Shared secret:")
			if errors.Is(err, u.ErrNoTerminal) {
				u.PrintFatal("setup needs --secret when there is no interactive terminal", nil)
			}
			if err != nil {
				u.PrintFatal("TUI error", err)
			}
			secret = entered
		}
		if secret == "" {
			u.PrintFatal("a shared secret is required", nil)
		}

		if err := u.SaveToken(&u.Token{ServerURL: setupFlags.serverURL, Secret: secret}); err != nil {
			u.PrintFatal("failed to write the credentials", err)
		}
		u.PrintSuccess("Credentials saved to ~/.config/element-agent/token.json")
	},
}

func init() {
	setupCmd.Flags().StringVarP(&setupFlags.serverURL, "server-url", "s", "", "Server base URL, including the path prefix")
	setupCmd.MarkFlagRequired("server-url")
	setupCmd.Flags().StringVar(&setupFlags.secret, "secret", "", "Shared registration secret")
}

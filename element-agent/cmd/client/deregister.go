package clientCmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"element-agent/internal/daemon"
	u "element-agent/utils"
)

var deregisterFlags struct {
	release bool
}

var deregisterCmd = &cobra.Command{
	Use:   "deregister <name>",
	Short: "Stop serving an agent, keeping its name and its directory",
	Long: `Stop serving an agent, keeping its name and its directory.

Mentions of the agent become no-ops and the name stays reserved for this machine, so
'client register' can start it again with different behaviour. Nothing local is
deleted. --release gives the name up as well, leaving it free for anyone to reserve.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		token := mustLoadToken()

		agent, err := mustStore().Load(name)
		if err != nil {
			u.PrintFatal(fmt.Sprintf("%s is not configured on this machine", name), err)
		}

		api := daemon.NewAPI(token.ServerURL, token.Secret)
		if err := api.Deregister(context.Background(), name, agent.Claim, deregisterFlags.release); err != nil {
			u.PrintFatal(fmt.Sprintf("failed to deregister %s", name), err)
		}

		if deregisterFlags.release {
			u.PrintSuccess(fmt.Sprintf("Released %s, the name is free to reserve again", name))
		} else {
			u.PrintSuccess(fmt.Sprintf("Stopped serving %s, its name and directory are unchanged", name))
		}
		notifyDaemon()
	},
}

func init() {
	deregisterCmd.Flags().BoolVar(&deregisterFlags.release, "release", false,
		"Give the name up so anyone can reserve it")
}

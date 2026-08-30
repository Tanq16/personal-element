package clientCmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"element-agent/internal/daemon"
	u "element-agent/utils"
)

var registerFlags struct {
	allowMessageRetrieval bool
}

var registerCmd = &cobra.Command{
	Use:   "register <name> -- <command> [args...]",
	Short: "Start serving a reserved agent from this machine",
	Long: `Start serving a reserved agent from this machine.

Everything after -- is the command the agent runs. The literal {{prompt}} in any
argument is replaced with the composed prompt before the command is executed. The
agent answers with whatever it writes to .result in its own directory, falling back
to standard output when that file is absent.

  element-agent client register reviewer --allow-message-retrieval -- \
    claude -p '{{prompt}}' --dangerously-skip-permissions --model haiku`,
	Args: cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name, argv := args[0], args[1:]
		token := mustLoadToken()
		store := mustStore()

		agent, err := store.Load(name)
		if err != nil {
			u.PrintFatal(fmt.Sprintf("%s is not reserved on this machine, run 'client init' first", name), err)
		}

		agent.Argv = argv
		agent.AllowMessageRetrieval = registerFlags.allowMessageRetrieval
		if err := store.Save(agent); err != nil {
			u.PrintFatal("failed to write the agent record", err)
		}

		api := daemon.NewAPI(token.ServerURL, token.Secret)
		if err := api.Register(context.Background(), name, agent.Claim); err != nil {
			u.PrintFatal(fmt.Sprintf("failed to register %s", name), err)
		}

		u.PrintSuccess(fmt.Sprintf("Serving %s", name))
		notifyDaemon()
	},
}

func init() {
	registerCmd.Flags().BoolVar(&registerFlags.allowMessageRetrieval, "allow-message-retrieval",
		false, "Let the agent read the conversation before the message it was mentioned in")
}

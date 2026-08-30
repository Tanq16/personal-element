package clientCmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"element-agent/internal/daemon"
	u "element-agent/utils"
)

var initCmd = &cobra.Command{
	Use:   "init <name>",
	Short: "Reserve an agent name and scaffold its directory",
	Long: `Reserve an agent name and scaffold its directory.

The name is claimed on the server and its Matrix account is created, but nothing
answers a mention until 'client register' is run. Write the instructions and skills
into the scaffolded directory first.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		token := mustLoadToken()

		reservation, err := daemon.NewAPI(token.ServerURL, token.Secret).Reserve(context.Background(), name)
		if err != nil {
			u.PrintFatal(fmt.Sprintf("failed to reserve %s", name), err)
		}

		store := mustStore()
		if err := store.Scaffold(daemon.Agent{Name: name, Claim: reservation.Claim}); err != nil {
			u.PrintFatal("failed to scaffold the agent directory", err)
		}

		dir := store.Dir(name)
		u.PrintSuccess(fmt.Sprintf("Reserved %s", reservation.UserID))
		u.PrintInfo(fmt.Sprintf("Write its instructions into %s", filepath.Join(dir, "AGENTS.md")))
		u.PrintInfo(fmt.Sprintf("Write its skills into %s", filepath.Join(dir, ".agents", "skills")))
		u.PrintInfo(fmt.Sprintf("Then run: element-agent client register %s -- <command>", name))
	},
}

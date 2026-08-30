package clientCmd

import (
	"github.com/spf13/cobra"

	"element-agent/internal/daemon"
	u "element-agent/utils"
)

var ClientCmd = &cobra.Command{
	Use:   "client",
	Short: "Reserve agents on this machine and run their jobs",
}

func init() {
	ClientCmd.AddCommand(setupCmd)
	ClientCmd.AddCommand(initCmd)
	ClientCmd.AddCommand(registerCmd)
	ClientCmd.AddCommand(deregisterCmd)
	ClientCmd.AddCommand(serveCmd)
}

func mustLoadToken() *u.Token {
	token, err := u.LoadToken()
	if err != nil {
		u.PrintFatal("no server credentials, run 'element-agent client setup' first", err)
	}
	return token
}

func mustConfigDir() string {
	dir, err := u.ConfigDir()
	if err != nil {
		u.PrintFatal("cannot resolve the config directory", err)
	}
	return dir
}

func mustStore() *daemon.Store {
	return daemon.NewStore(mustConfigDir())
}

func notifyDaemon() {
	if err := daemon.Reload(); err != nil {
		u.PrintInfo("The daemon is not running, start it with 'element-agent client serve'")
		return
	}
	u.PrintInfo("The running daemon picked up the change")
}

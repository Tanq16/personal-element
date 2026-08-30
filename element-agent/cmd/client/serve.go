package clientCmd

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"element-agent/internal/daemon"
	u "element-agent/utils"
)

var serveFlags struct {
	timeout time.Duration
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the daemon that executes jobs for this machine's agents",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		token := mustLoadToken()

		d, err := daemon.New(daemon.Config{
			ServerURL: token.ServerURL,
			Secret:    token.Secret,
			Root:      mustConfigDir(),
			Loopback:  daemon.LoopbackAddr,
			Timeout:   serveFlags.timeout,
		})
		if errors.Is(err, daemon.ErrDaemonRunning) {
			u.PrintFatal("the daemon is already running on "+daemon.LoopbackAddr, nil)
		}
		if err != nil {
			u.PrintFatal("failed to start the daemon", err)
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		if err := d.Run(ctx); err != nil {
			u.PrintFatal("daemon stopped", err)
		}
	},
}

func init() {
	serveCmd.Flags().DurationVarP(&serveFlags.timeout, "timeout", "T", 300*time.Second, "Wall clock limit for a single job")
}

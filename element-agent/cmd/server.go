package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"element-agent/internal/server"
)

var serverFlags struct {
	config string
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run the Matrix application service",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := server.LoadConfig(serverFlags.config)
		if err != nil {
			log.Fatal().Err(err).Str("config", serverFlags.config).Msg("failed to load configuration")
		}

		srv, err := server.New(cfg)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to start")
		}
		srv.Setup()

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		if err := srv.Run(ctx); err != nil {
			log.Fatal().Err(err).Msg("server stopped")
		}
	},
}

func init() {
	serverCmd.Flags().StringVarP(&serverFlags.config, "config", "c", "/etc/element-agent/config.yaml", "Path to the server configuration file")
}

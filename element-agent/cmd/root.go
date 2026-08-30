package cmd

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	clientCmd "element-agent/cmd/client"
	"element-agent/utils"
)

var AppVersion = "dev-build"

var debugFlag bool

var rootCmd = &cobra.Command{
	Use:               "element-agent",
	Short:             "Matrix application service and local job runner for @agent_ accounts",
	Version:           AppVersion,
	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func setupLogs() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.DateTime, NoColor: !utils.StdoutIsTerminal}
	log.Logger = zerolog.New(output).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debugFlag {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		utils.GlobalDebugFlag = true
	}
}

func init() {
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})

	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Enable debug logging")

	cobra.OnInitialize(setupLogs)

	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(clientCmd.ClientCmd)
}

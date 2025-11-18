package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/TylerHendrickson/paprika"
	"github.com/alecthomas/kong"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
)

// reportedErr is a wrapper for errors that do not need to be reported by Kong.
type reportedErr struct {
	error
}

// CLI is the command-line application root.
type CLI struct {
	Version     kong.VersionFlag `help:"Print version information and exit." short:"v"`
	VersionFull VersionFullFlag  `help:"Print detailed version information and exit."`

	DataDir string `help:"Path for the directory used to store Paprika data." env:"PAPRIKA_DATA_DIR" type:"existingdir" default:"data"`

	PaprikaUsername string   `help:"Username for Paprika API auth." env:"PAPRIKA_USER"`
	PaprikaPassword string   `help:"Password for Paprika API auth." env:"PAPRIKA_PASSWORD"`
	PaprikaBaseURL  *url.URL `help:"Base URL for the Paprika API." env:"PAPRIKA_BASE_URL" hidden:""`

	Sync SyncCMD `cmd:"" name:"sync" help:"Sync (backup) data from the Paprika API to the local file system."`

	LoggingOpts struct {
		Level  zerolog.Level `help:"Minimum log level. [default: ${default}] " enum:"${logLevelEnum}" default:"warn" env:"LOG_LEVEL"`
		Format struct {
			Pretty bool `help:"Force pretty log output. [default: (enabled if stderr is a TTY.)] " xor:"logfmt" env:"LOG_PRETTY"`
			JSON   bool `help:"Force JSON log output. [default: (enabled if stderr is not a TTY.)]" xor:"logfmt" env:"LOG_JSON"`
		} `embed:""`
		TimestampLayout string `help:"Layout for formatting logged timestamps. Expects a Go time layout string. [default: \"${default}\" (${logTimestampDefaultName})] " default:"${logTimestampDefaultLayout}" placeholder:"LAYOUT" env:"LOG_TIMESTAMP_LAYOUT"`
		NoColor         bool   `help:"Disable colorized log output (affects pretty logs only). " default:"false" env:"NO_COLOR,LOG_NO_COLOR"`
	} `embed:"" prefix:"log-" group:"Logging Options" description:"Control Logging Behaviors"`

	// Not controllable through CLI arguments:
	// CLI output streams
	stdout, stderr *os.File
}

// newLogger creates and returns a new logger according to the CLI configuration state.
func (cli *CLI) newLogger() zerolog.Logger {
	zerolog.TimeFieldFormat = cli.LoggingOpts.TimestampLayout
	var logWriter io.Writer = cli.stderr
	if (isatty.IsTerminal(cli.stderr.Fd()) || cli.LoggingOpts.Format.Pretty) && !cli.LoggingOpts.Format.JSON {
		logWriter = zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
			w.Out = logWriter
			w.TimeFormat = cli.LoggingOpts.TimestampLayout
			w.NoColor = cli.LoggingOpts.NoColor
		})
	}
	logger := zerolog.New(logWriter).With().
		Timestamp().
		Logger().
		Level(zerolog.Level(cli.LoggingOpts.Level))
	if logger.GetLevel() == zerolog.TraceLevel {
		// Add caller to all logs when minimum log level is trace
		logger = logger.With().Caller().Logger()
	}
	return logger
}

// AfterApply is a hook that configures the application after parsing.
func (cli *CLI) AfterApply(ctx context.Context, kctx *kong.Context) error {
	kctx.Bind(cli)
	logger := cli.newLogger().With().Str("dataDir", cli.DataDir).Logger()
	kctx.Bind(logger)
	var (
		paprikaClient    *paprika.Client
		paprikaClientErr error
	)
	if cli.PaprikaBaseURL != nil {
		paprikaClient, paprikaClientErr = paprika.NewClientWithURL(cli.PaprikaUsername, cli.PaprikaPassword, cli.PaprikaBaseURL)
	} else {
		paprikaClient, paprikaClientErr = paprika.NewClient(cli.PaprikaUsername, cli.PaprikaPassword)
	}
	if paprikaClientErr != nil {
		return fmt.Errorf("failed to create Paprika API client: %w", paprikaClientErr)
	}
	kctx.Bind(paprikaClient)

	logger.Debug().
		// zerolog.Array.Type() does not exist; see https://github.com/rs/zerolog/issues/729
		// Array("bound-types", zerolog.Arr().Type(logger).Type(reader).Type(writer)).
		Array("bound-types", zerolog.Arr().
			Str(fmt.Sprintf("%T", cli)).
			Str(fmt.Sprintf("%T", logger)).
			Str(fmt.Sprintf("%T", paprikaClient)),
		).Msg("adding bindings to application context")

	logger.Trace().Interface("configuration", cli).Msg("dump final application configuration")
	return nil
}

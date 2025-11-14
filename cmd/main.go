package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/alecthomas/kong"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

func main() {
	if err := godotenv.Load(); err != nil {
		panic(err)
	}

	// Register context to allow graceful shutdown on SIGINT.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	// If signaled, unregister to restore default behavior and allow any
	// subsequent SIGINT to exit immediately.
	go func() { <-ctx.Done(); stop() }()
	defer stop()

	// Run the program
	Main(ctx, os.Stdout, os.Stderr, os.Args[1:], os.Exit)
}

func Main(ctx context.Context, stdout, stderr *os.File, args []string, exit func(int)) {
	var cli CLI
	cli.stdout = stdout
	cli.stderr = stderr
	kctx := Parse(
		&cli, args,
		kong.Description("Restructures CSV into JSON."),
		kong.ShortUsageOnError(),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.Vars{
			"version":                   versionStringShort(),
			"defaultLogLevelName":       zerolog.WarnLevel.String(),
			"logTimestampDefaultName":   "RFC3339",
			"logTimestampDefaultLayout": time.RFC3339,
			"logLevelEnum": enumTag(
				zerolog.TraceLevel,
				zerolog.DebugLevel,
				zerolog.InfoLevel,
				zerolog.WarnLevel,
				zerolog.ErrorLevel,
				zerolog.FatalLevel,
				zerolog.PanicLevel,
			),
		},
		kong.Exit(exit),
	)

	if cli.VersionFull {
		// Print detailed version and exit
		fmt.Fprintln(kctx.Stdout, versionStringFull())
		kctx.Exit(0)
	}

	if err := kctx.Run(); err != nil {
		var re reportedErr
		if errors.As(err, &re) {
			kctx.Exit(1)
		}
		kctx.FatalIfErrorf(err)
	}
}

// Parse mirrors kong.Parse(), but parses osArgs instead of os.Args[1:]
func Parse(cli any, osArgs []string, options ...kong.Option) *kong.Context {
	parser, err := kong.New(cli, options...)
	if err != nil {
		panic(err)
	}
	ctx, err := parser.Parse(osArgs)
	parser.FatalIfErrorf(err)
	return ctx
}

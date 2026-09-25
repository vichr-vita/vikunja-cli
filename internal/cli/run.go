package cli

import (
	"context"
	"io"
	"net/http"
	"os"
)

type Options struct {
	Args        []string
	Stdout      io.Writer
	Stderr      io.Writer
	HTTPClient  *http.Client
	Getenv      func(string) string
	UserHomeDir func() (string, error)
	Context     context.Context
}

func Run(ctx context.Context, options Options) int {
	options = withDefaults(ctx, options)
	cmd, help, parseErr := parseCommand(options.Args)
	if parseErr != nil {
		writeCLIError(options.Stderr, parseErr)
		return parseErr.ExitCode
	}
	if help != nil {
		if err := writeJSON(options.Stdout, help); err != nil {
			failure := internalError()
			writeCLIError(options.Stderr, failure)
			return failure.ExitCode
		}
		return 0
	}

	path, pathErr := configPath(options.Getenv, options.UserHomeDir)
	if pathErr != nil {
		writeCLIError(options.Stderr, pathErr)
		return pathErr.ExitCode
	}
	cfg, configErr := loadConfig(path)
	if configErr != nil {
		writeCLIError(options.Stderr, configErr)
		return configErr.ExitCode
	}
	response, requestErr := executeRequest(options, cfg, cmd)
	if requestErr != nil {
		writeCLIError(options.Stderr, requestErr)
		return requestErr.ExitCode
	}
	if err := writeJSON(options.Stdout, response); err != nil {
		failure := internalError()
		writeCLIError(options.Stderr, failure)
		return failure.ExitCode
	}
	return 0
}

func withDefaults(ctx context.Context, options Options) Options {
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	if options.Getenv == nil {
		options.Getenv = os.Getenv
	}
	if options.UserHomeDir == nil {
		options.UserHomeDir = os.UserHomeDir
	}
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	if ctx == nil {
		ctx = context.Background()
	}
	options.Context = ctx
	return options
}

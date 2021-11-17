package main

import (
	"log"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/pkg/errors"

	"github.com/smartcontractkit/chainlink/core/cmd"
	"github.com/smartcontractkit/chainlink/core/config"
	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/smartcontractkit/chainlink/core/sessions"
	"github.com/smartcontractkit/chainlink/core/static"
)

func main() {
	env := "prod"
	if os.Getenv("CHAINLINK_DEV") == "true" {
		env = "dev"
	}
	err := sentry.Init(sentry.ClientOptions{
		// AttachStacktrace is needed to send stacktrace alongside panics
		AttachStacktrace: true,
		// If static.SentryDSN has been set, will use that, otherwise uses
		// SENTRY_DSN env var. If neither are set, sentry is disabled.
		Dsn: static.SentryDSN,
		// Either set environment and release here or set the SENTRY_ENVIRONMENT
		// and SENTRY_RELEASE environment variables.
		Environment: env,
		Release:     static.Version,
		// Enable printing of SDK debug messages.
		// Uncomment line below to debug sentry
		// Debug: true,
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	func() {
		// Flush buffered events before the program terminates.
		defer sentry.Flush(2 * time.Second)
		// Catch panics and report
		defer sentry.Recover()
		Run(NewProductionClient(), os.Args...)
	}()
}

// Run runs the CLI, providing further command instructions by default.
func Run(client *cmd.Client, args ...string) {
	app := cmd.NewApp(client)
	client.Logger.ErrorIf(app.Run(args), "Error running app")
}

// NewProductionClient configures an instance of the CLI to be used
// in production.
func NewProductionClient() *cmd.Client {
	cfg := config.NewGeneralConfig()
	lggr := logger.NewLogger(cfg)

	prompter := cmd.NewTerminalPrompter()
	cookieAuth := cmd.NewSessionCookieAuthenticator(cfg, cmd.DiskCookieStore{Config: cfg}, lggr)
	sr := sessions.SessionRequest{}
	sessionRequestBuilder := cmd.NewFileSessionRequestBuilder(lggr)
	if credentialsFile := cfg.AdminCredentialsFile(); credentialsFile != "" {
		var err error
		sr, err = sessionRequestBuilder.Build(credentialsFile)
		if err != nil && errors.Cause(err) != cmd.ErrNoCredentialFile && !os.IsNotExist(err) {
			lggr.Fatalw("Error loading API credentials", "error", err, "credentialsFile", credentialsFile)
		}
	}
	return &cmd.Client{
		Renderer:                       cmd.RendererTable{Writer: os.Stdout},
		Config:                         cfg,
		Logger:                         lggr,
		AppFactory:                     cmd.ChainlinkAppFactory{},
		KeyStoreAuthenticator:          cmd.TerminalKeyStoreAuthenticator{Prompter: prompter},
		FallbackAPIInitializer:         cmd.NewPromptingAPIInitializer(prompter),
		Runner:                         cmd.ChainlinkRunner{},
		HTTP:                           cmd.NewAuthenticatedHTTPClient(cfg, cookieAuth, sr),
		CookieAuthenticator:            cookieAuth,
		FileSessionRequestBuilder:      sessionRequestBuilder,
		PromptingSessionRequestBuilder: cmd.NewPromptingSessionRequestBuilder(prompter),
		ChangePasswordPrompter:         cmd.NewChangePasswordPrompter(),
		PasswordPrompter:               cmd.NewPasswordPrompter(),
	}
}

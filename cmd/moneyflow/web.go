package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/version"
	webserver "github.com/wesm/moneyflow/internal/web"
)

// ListenerFactory opens one network listener without fixing a port in tests.
type ListenerFactory func(context.Context, string, string) (net.Listener, error)

// BrowserOpener opens one URL without invoking a shell.
type BrowserOpener func(string) error

// SignalContext adds process lifecycle signals to a command context.
type SignalContext func(context.Context) (context.Context, context.CancelFunc)

// WebRunner runs the browser transport.
type WebRunner func(context.Context, *app.Service, WebOptions, IOStreams) error

// WebOptions contains the explicitly bounded web-command configuration.
type WebOptions struct {
	Listen      string
	BasePath    string
	ExternalURL string
	Open        bool
}

var dnsLabelPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

func newWebCommand(streams IOStreams) *cobra.Command {
	options := WebOptions{Listen: "127.0.0.1:8080", BasePath: "/", Open: true}
	var demo bool
	var fixturePath string
	command := &cobra.Command{
		Use:   "web",
		Short: "Serve the browser application",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			host, err := validateWebListen(options.Listen)
			if err != nil {
				return fmt.Errorf("start web: %w", err)
			}
			options.BasePath, err = api.NormalizeBasePath(options.BasePath)
			if err != nil {
				return fmt.Errorf("start web: %w", err)
			}
			if _, err = api.ResolveOrigin(options.Listen, options.BasePath, options.ExternalURL); err != nil {
				return fmt.Errorf("start web: %w", err)
			}
			opener := streams.OpenProfile
			if opener == nil {
				opener = openProfile
			}
			opened, err := opener(command.Context(), ProfileOptions{
				Demo: demo || fixturePath != "", FixturePath: fixturePath,
			})
			if err != nil {
				return fmt.Errorf("start web: %w", err)
			}
			if err = configureOpenedMonarchProvider(
				command.Context(), opened, streams, "web",
			); err != nil {
				return fmt.Errorf("start web: %w", closeOpenedProfile(opened, err))
			}
			if !isLoopbackHost(host) {
				_, _ = fmt.Fprintln(
					command.ErrOrStderr(),
					"Warning: serving unauthenticated financial data on a non-loopback address.",
				)
			}
			runner := streams.RunWeb
			if runner == nil {
				runner = runWeb
			}
			runErr := runner(command.Context(), opened.Service, options, streams)
			if err = closeOpenedProfile(opened, runErr); err != nil {
				return fmt.Errorf("start web: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&options.Listen, "listen", options.Listen, "explicit host and port")
	command.Flags().StringVar(&options.BasePath, "base-path", options.BasePath, "URL mount path")
	command.Flags().StringVar(&options.ExternalURL, "external-url", "", "canonical browser URL through a trusted proxy")
	command.Flags().BoolVar(&options.Open, "open", options.Open, "open the application in a browser")
	command.Flags().BoolVar(&demo, "demo", false, "serve a temporary profile seeded with synthetic data")
	command.Flags().StringVar(&fixturePath, "fixture", "", "fixture document")
	if err := command.Flags().MarkHidden("fixture"); err != nil {
		panic(err)
	}
	return command
}

func validateWebListen(address string) (string, error) {
	if strings.ContainsAny(address, "/?#\\\x00\r\n") {
		return "", errors.New("listen address must contain only a host and port")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid listen address: %w", err)
	}
	if host == "" {
		return "", errors.New("listen host is required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("listen port must be between 1 and 65535")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() {
			return "", errors.New("wildcard listen addresses are forbidden")
		}
		return host, nil
	}
	if len(host) > 253 {
		return "", errors.New("listen host is not a valid IP address or DNS name")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) > 63 || !dnsLabelPattern.MatchString(label) {
			return "", errors.New("listen host is not a valid IP address or DNS name")
		}
	}
	return host, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func runWeb(
	parent context.Context,
	service *app.Service,
	options WebOptions,
	streams IOStreams,
) error {
	basePath, err := api.NormalizeBasePath(options.BasePath)
	if err != nil {
		return fmt.Errorf("normalize base path: %w", err)
	}
	options.BasePath = basePath
	signalContext := streams.SignalContext
	if signalContext == nil {
		signalContext = func(parent context.Context) (context.Context, context.CancelFunc) {
			return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
		}
	}
	ctx, stop := signalContext(parent)
	defer stop()

	listen := streams.Listen
	if listen == nil {
		listenConfig := &net.ListenConfig{}
		listen = listenConfig.Listen
	}
	listener, err := listen(ctx, "tcp", options.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", options.Listen, err)
	}
	origin, err := api.ResolveOrigin(listener.Addr().String(), options.BasePath, options.ExternalURL)
	if err != nil {
		_ = listener.Close()
		return err
	}
	security, err := api.NewMutationSecurity(origin, nil, nil)
	if err != nil {
		_ = listener.Close()
		return err
	}
	application, err := webserver.NewServer(webserver.ServerConfig{
		Service: service, BasePath: options.BasePath, Version: version.Version,
		Origin: origin, Security: security, WarnNonCanonical: options.ExternalURL != "",
	})
	if err != nil {
		_ = listener.Close()
		return err
	}
	server := application.HTTPServer(listener.Addr().String(), streams.Err)
	url := origin.Canonical.String()
	if _, err := fmt.Fprintf(streams.Out, "Moneyflow web: %s\n", url); err != nil {
		_ = listener.Close()
		return fmt.Errorf("write web address: %w", err)
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()
	if options.Open {
		opener := streams.OpenBrowser
		if opener == nil {
			opener = openBrowser
		}
		if err := opener(url); err != nil {
			_, _ = fmt.Fprintf(streams.Err, "Warning: could not open browser: %v\n", err)
		}
	}

	select {
	case err := <-serveResult:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve web: %w", err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
			return fmt.Errorf("shut down web: %w", err)
		}
		err := <-serveResult
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve web: %w", err)
		}
		return nil
	}
}

func openBrowser(url string) error {
	var name string
	var arguments []string
	switch runtime.GOOS {
	case "darwin":
		name, arguments = "open", []string{url}
	case "windows":
		name, arguments = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, arguments = "xdg-open", []string{url}
	}
	// The executable is selected from fixed platform names and the validated URL is one argument.
	return exec.Command(name, arguments...).Run() //nolint:gosec
}

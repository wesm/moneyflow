package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/wesm/moneyflow/internal/httpsecurity"
	"github.com/wesm/moneyflow/internal/mcp"
)

const (
	// MCPTransportStdio serves protocol frames on stdin/stdout.
	MCPTransportStdio = "stdio"
	// MCPTransportStreamableHTTP serves authenticated stateless JSON responses.
	MCPTransportStreamableHTTP = "streamable-http"
)

// MCPOptions contains the explicitly bounded MCP transport configuration.
type MCPOptions struct {
	AllowWrite  bool
	Unlock      bool
	Transport   string
	Listen      string
	BasePath    string
	ExternalURL string
}

// MCPRunner serves one already-opened profile until transport shutdown.
type MCPRunner func(context.Context, MCPDependencies, MCPOptions, IOStreams) error

func newMCPCommand(streams IOStreams) *cobra.Command {
	options := MCPOptions{
		Transport: MCPTransportStdio, Listen: "127.0.0.1:8081", BasePath: "/mcp/",
	}
	var profile string
	command := &cobra.Command{
		Use:   "mcp",
		Short: "Serve one profile through the Model Context Protocol",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if options.Transport != MCPTransportStdio && options.Transport != MCPTransportStreamableHTTP {
				return fmt.Errorf("start MCP: unsupported transport %q", options.Transport) //nolint:revive // product name
			}
			if options.Transport == MCPTransportStdio &&
				(command.Flags().Changed("listen") || command.Flags().Changed("base-path") ||
					command.Flags().Changed("external-url")) {
				return errors.New("start MCP: HTTP flags require --transport streamable-http") //nolint:revive // product name
			}
			if options.Transport == MCPTransportStreamableHTTP {
				if _, err := httpsecurity.ValidateListen(options.Listen, true); err != nil {
					return fmt.Errorf("start MCP: %w", err) //nolint:revive // product name
				}
				var err error
				options.BasePath, err = httpsecurity.NormalizeBasePath(options.BasePath)
				if err != nil {
					return fmt.Errorf("start MCP: %w", err) //nolint:revive // product name
				}
				if _, err = httpsecurity.ResolveOrigin(
					options.Listen, options.BasePath, options.ExternalURL,
				); err != nil {
					return fmt.Errorf("start MCP: %w", err) //nolint:revive // product name
				}
			}
			builder := streams.BuildMCP
			if builder == nil {
				builder = buildMCPDependencies
			}
			dependencies, err := builder(
				command.Context(), ProfileOptions{Profile: profile}, options, streams,
			)
			if err != nil {
				return fmt.Errorf("start MCP: %w", err) //nolint:revive // product name
			}
			runner := streams.RunMCP
			if runner == nil {
				runner = runMCP
			}
			runErr := runner(command.Context(), dependencies, options, streams)
			closeContext, cancel := context.WithTimeout(context.Background(), mcp.HTTPShutdownTimeout)
			defer cancel()
			if err = errors.Join(runErr, dependencies.Close(closeContext)); err != nil {
				return fmt.Errorf("start MCP: %w", err) //nolint:revive // product name
			}
			return nil
		},
	}
	command.Flags().StringVar(&profile, "profile", "", "profile name or ID")
	command.Flags().StringVar(&options.Transport, "transport", options.Transport, "stdio or streamable-http")
	command.Flags().BoolVar(&options.AllowWrite, "allow-write", false, "register staged editing tools")
	command.Flags().BoolVar(&options.Unlock, "unlock", false, "unlock the YNAB vault through the controlling terminal (does not imply --allow-write)")
	command.Flags().StringVar(&options.Listen, "listen", options.Listen, "loopback host and port")
	command.Flags().StringVar(&options.BasePath, "base-path", options.BasePath, "exact HTTP endpoint path")
	command.Flags().StringVar(&options.ExternalURL, "external-url", "", "canonical URL through a trusted proxy")
	command.AddCommand(newMCPTokenCommand())
	return command
}

func newMCPTokenCommand() *cobra.Command {
	token := &cobra.Command{
		Use:   "token",
		Short: "Manage the profile-scoped streamable-HTTP bearer token",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	for _, rotate := range []bool{false, true} {
		rotate := rotate
		verb := "reveal"
		short := "Reveal the current bearer token"
		if rotate {
			verb = "rotate"
			short = "Rotate and reveal the bearer token"
		}
		var profile string
		child := &cobra.Command{
			Use: verb, Short: short, Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				store, release, err := resolveMCPTokenStore(command.Context(), "", profile)
				if err != nil {
					return fmt.Errorf("%s MCP token: %w", verb, err) //nolint:revive // product name
				}
				var value string
				if rotate {
					value, err = store.Rotate()
				} else {
					value, err = store.Reveal()
				}
				if err != nil {
					return errors.Join(
						fmt.Errorf("%s MCP token: %w", verb, err), //nolint:revive // product name
						release(),
					)
				}
				if err = release(); err != nil {
					return fmt.Errorf("%s MCP token: release profile: %w", verb, err) //nolint:revive // product name
				}
				_, err = fmt.Fprintln(command.OutOrStdout(), value)
				return err
			},
		}
		child.Flags().StringVar(&profile, "profile", "", "profile name or ID")
		token.AddCommand(child)
	}
	return token
}

func runMCP(
	parent context.Context,
	dependencies MCPDependencies,
	options MCPOptions,
	streams IOStreams,
) error {
	if dependencies.Server == nil {
		return errors.New("MCP server is unavailable") //nolint:revive // product name
	}
	signalContext := streams.SignalContext
	if signalContext == nil {
		signalContext = func(parent context.Context) (context.Context, context.CancelFunc) {
			return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
		}
	}
	ctx, stop := signalContext(parent)
	defer stop()
	if options.Transport == MCPTransportStdio {
		return dependencies.Server.RunStdio(ctx)
	}
	if dependencies.TokenStore == nil {
		return errors.New("MCP HTTP token store is unavailable") //nolint:revive // product name
	}
	listen := streams.Listen
	if listen == nil {
		listen = (&net.ListenConfig{}).Listen
	}
	listener, err := listen(ctx, "tcp", options.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", options.Listen, err)
	}
	if _, err = httpsecurity.ValidateListen(listener.Addr().String(), true); err != nil {
		_ = listener.Close()
		return fmt.Errorf("bound MCP listener is not loopback: %w", err) //nolint:revive // product name
	}
	originListen := listener.Addr().String()
	if options.ExternalURL != "" {
		originListen = options.Listen
	}
	origin, err := httpsecurity.ResolveOrigin(originListen, options.BasePath, options.ExternalURL)
	if err != nil {
		_ = listener.Close()
		return err
	}
	if _, err = dependencies.TokenStore.Reveal(); err != nil {
		_ = listener.Close()
		return err
	}
	handler, err := dependencies.Server.HTTPHandler(mcp.HTTPOptions{
		Origin: origin, TokenStore: dependencies.TokenStore,
	})
	if err != nil {
		_ = listener.Close()
		return err
	}
	if _, err = fmt.Fprintf(
		streams.Err, "Moneyflow MCP: %s\nBearer token file: %s\n",
		origin.Canonical.String(), dependencies.TokenStore.Path(),
	); err != nil {
		_ = listener.Close()
		return err
	}
	httpServer := mcp.NewHTTPServer(listener.Addr().String(), handler)
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	select {
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), mcp.HTTPShutdownTimeout)
		defer cancel()
		shutdownErr := httpServer.Shutdown(shutdownContext)
		serveErr := <-served
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		return errors.Join(shutdownErr, serveErr)
	}
}

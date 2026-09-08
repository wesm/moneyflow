package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

// MCPDependencies owns one selected profile and its process-scoped MCP server.
type MCPDependencies struct {
	Server     *mcp.Server
	TokenStore *mcp.TokenStore
	ProfileID  string
	close      func(context.Context) error
}

// Close stops process-owned work before releasing the selected profile.
func (dependencies MCPDependencies) Close(ctx context.Context) error {
	if dependencies.close == nil {
		return nil
	}
	return dependencies.close(ctx)
}

// MCPDependencyBuilder resolves and opens one persistent profile without onboarding.
type MCPDependencyBuilder func(
	context.Context,
	ProfileOptions,
	MCPOptions,
	IOStreams,
) (MCPDependencies, error)

func buildMCPDependencies(
	ctx context.Context,
	options ProfileOptions,
	mcpOptions MCPOptions,
	streams IOStreams,
) (MCPDependencies, error) {
	catalog, entry, err := resolveMCPProfile(ctx, options.ExplicitHome, options.Profile)
	if err != nil {
		return MCPDependencies{}, err
	}
	opener := streams.OpenProfile
	if opener == nil {
		opener = openProfile
	}
	opened, err := opener(ctx, ProfileOptions{
		ExplicitHome: options.ExplicitHome, Profile: entry.ID,
	})
	if err != nil {
		return MCPDependencies{}, err
	}
	if mcpOptions.Unlock {
		err = unlockMCPProvider(ctx, opened, streams)
	} else {
		err = configureOpenedProvider(ctx, opened, streams, "mcp")
	}
	if err != nil {
		return MCPDependencies{}, closeOpenedProfile(opened, err)
	}
	matcher, err := newCatalogAmazonMatcher(catalog)
	if err != nil {
		return MCPDependencies{}, closeOpenedProfile(opened, err)
	}
	opened.Service.ConfigureAmazonMatching(matcher)
	tokenStore, err := mcp.NewTokenStore(opened.Paths.Root, cryptorand.Reader)
	if err != nil {
		return MCPDependencies{}, closeOpenedProfile(opened, err)
	}
	server, err := mcp.New(mcp.Dependencies{
		Service: opened.Service, ProfileID: opened.ID, ProfileName: entry.DisplayName,
		ProfileRoot: opened.Paths.Root, Clock: time.Now, Random: cryptorand.Reader,
		Logger: newMCPLogger(streams.Err),
	}, mcp.Options{AllowWrite: mcpOptions.AllowWrite})
	if err != nil {
		return MCPDependencies{}, closeOpenedProfile(opened, err)
	}
	return MCPDependencies{
		Server: server, TokenStore: tokenStore, ProfileID: opened.ID,
		close: func(closeContext context.Context) error {
			return errors.Join(server.Close(closeContext), opened.Close())
		},
	}, nil
}

func resolveMCPProfile(
	ctx context.Context,
	explicitHome string,
	selector string,
) (*profilecatalog.Catalog, profilecatalog.Entry, error) {
	catalog, err := openProfileCatalog(explicitHome)
	if err != nil {
		return nil, profilecatalog.Entry{}, err
	}
	entries, err := catalog.List(ctx)
	if err != nil {
		return nil, profilecatalog.Entry{}, err
	}
	entry, err := profilecatalog.ResolveEntries(entries, selector)
	if err != nil {
		return nil, profilecatalog.Entry{}, err
	}
	if entry.ID == "" {
		entry, err = catalog.Activate(ctx, entry.Key)
		if err != nil {
			return nil, profilecatalog.Entry{}, err
		}
	}
	return catalog, entry, nil
}

// resolveMCPTokenStore resolves one profile and returns its token store together with the shared
// profile lifecycle lock that must stay held while the token is revealed or rotated. Holding the
// lock keeps a concurrent profile cancellation from quarantining the root mid-operation, which
// would otherwise let token creation recreate that root as a manifest-less partial profile.
func resolveMCPTokenStore(
	ctx context.Context,
	explicitHome string,
	selector string,
) (*mcp.TokenStore, func() error, error) {
	catalog, entry, err := resolveMCPProfile(ctx, explicitHome, selector)
	if err != nil {
		return nil, nil, err
	}
	lifecycle, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return nil, nil, err
	}
	if err = catalog.ValidateEntry(entry); err != nil {
		return nil, nil, errors.Join(err, lifecycle.Release())
	}
	store, err := mcp.NewTokenStore(entry.Root, cryptorand.Reader)
	if err != nil {
		return nil, nil, errors.Join(err, lifecycle.Release())
	}
	return store, lifecycle.Release, nil
}

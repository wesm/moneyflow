package mcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wesm/moneyflow/internal/access"
	"github.com/wesm/moneyflow/internal/app"
)

// ProfileArgument selects the profile one tool call reads or changes.
type ProfileArgument struct {
	Profile string `json:"profile,omitempty" jsonschema:"profile ID; optional when only one exists"`
}

func (argument ProfileArgument) selectedProfile() string { return argument.Profile }

type profileSelector interface{ selectedProfile() string }

// ProfileSummary is one list_profiles entry.
type ProfileSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	State    string `json:"state"`
}

type profileAcquirer interface {
	Acquire(ctx context.Context, profile string) (Dependencies, func(), error)
}

// Profiles resolves the profile a tool call names. Acquire with an empty profile selects the
// only profile; it fails with ErrProfileRequired when there are several and ErrProfileNotFound
// when none matches, and with an UnavailableError when the profile cannot open. Every call for
// one profile returns the same Supervisor. The returned function releases the profile.
type Profiles interface {
	List(ctx context.Context) ([]ProfileSummary, error)
	Acquire(ctx context.Context, profile string) (Dependencies, func(), error)
}

var (
	// ErrProfileRequired reports a call without a profile while several profiles exist.
	ErrProfileRequired = errors.New("a profile argument is required")
	// ErrProfileNotFound reports a profile argument that names no profile.
	ErrProfileNotFound = errors.New("profile not found")
	// ErrProfileUnavailable reports a profile that exists but cannot open.
	ErrProfileUnavailable = errors.New("profile is unavailable")

	errToolPanicked = errors.New("MCP tool stopped unexpectedly") //nolint:revive // product name
)

// UnavailableError reports a profile that cannot open for a reason that is safe to show to the
// client, such as the catalog's "profile_busy: …". It matches ErrProfileUnavailable.
type UnavailableError struct{ Reason string }

func (failure UnavailableError) Error() string {
	return ErrProfileUnavailable.Error() + ": " + failure.Reason
}

// Unwrap lets errors.Is match ErrProfileUnavailable.
func (UnavailableError) Unwrap() error { return ErrProfileUnavailable }

// RegisterOptions selects the tools Register adds.
type RegisterOptions struct {
	// Classes lists the classes to register. It must not be empty.
	Classes []access.Class
	// Prefix is prepended to every tool name and matches ^[A-Za-z0-9_.-]*$.
	Prefix string
}

var toolPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]*$`)

var toolClasses = map[string]access.Class{
	"search_transactions":            access.Read,
	"get_transactions":               access.Read,
	"get_spending_summary":           access.Read,
	"get_insights":                   access.Read,
	"get_categories":                 access.Read,
	"get_merchants":                  access.Read,
	"get_account_info":               access.Read,
	"get_uncategorized_transactions": access.Read,
	"get_amazon_order_details":       access.Read,
	"get_transaction_details":        access.Read,
	"review_changes":                 access.Read,
	"get_commit_status":              access.Read,
	"get_refresh_status":             access.Read,
	"get_reconcile_status":           access.Read,
	"preview_export":                 access.Read,
	"list_profiles":                  access.Read,
	"refresh_data":                   access.Stage,
	"export_transactions":            access.Stage,
	"update_transaction_category":    access.Stage,
	"batch_update_category":          access.Stage,
	"undo_changes":                   access.Stage,
	"redo_changes":                   access.Stage,
	"reassign_transactions_merchant": access.Stage,
	"rename_merchant":                access.Stage,
	"toggle_transactions_hidden":     access.Stage,
	"delete_transactions":            access.Stage,
	"manage_category":                access.Stage,
	"manage_category_group":          access.Stage,
	"commit_changes":                 access.Commit,
	"pause_commit":                   access.Commit,
	"resume_commit":                  access.Commit,
	"stop_and_reconcile":             access.Commit,
	"confirm_reconcile":              access.Commit,
	"confirm_refresh_deletions":      access.Commit,
}

// Register adds moneyflow's tools for the selected classes to sdk. Every tool except
// list_profiles takes an optional profile argument that profiles resolves on each call.
// Register adds no resources and no middleware.
func Register(sdk *mcpsdk.Server, profiles Profiles, options RegisterOptions) error {
	if sdk == nil || profiles == nil {
		return errors.New("register MCP tools: server and profiles are required")
	}
	if len(options.Classes) == 0 {
		return errors.New("register MCP tools: choose at least one class")
	}
	classes := make(map[access.Class]bool, len(options.Classes))
	for _, class := range options.Classes {
		if !class.Valid() {
			return fmt.Errorf("register MCP tools: unknown class %q", string(class))
		}
		classes[class] = true
	}
	if !toolPrefixPattern.MatchString(options.Prefix) {
		return fmt.Errorf("register MCP tools: prefix %q must match %s",
			options.Prefix, toolPrefixPattern)
	}
	server := &Server{SDK: sdk, profiles: profiles, prefix: options.Prefix, classes: classes}
	if classes[access.Read] {
		registerListProfiles(server, profiles)
	}
	registerReadTools(server)
	registerExportTools(server)
	registerWriteTools(server)
	return nil
}

// ListProfilesInput takes no arguments.
type ListProfilesInput struct{}

// ProfilesDocument lists the profiles other tools accept.
type ProfilesDocument struct {
	Header
	Profiles []ProfileSummary `json:"profiles"`
}

func registerListProfiles(server *Server, profiles Profiles) {
	destructive := false
	mcpsdk.AddTool[ListProfilesInput, any](server.SDK, &mcpsdk.Tool{
		Name:        server.prefix + "list_profiles",
		Description: "List the profiles that other tools accept in their profile argument.",
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &destructive},
	}, func(
		ctx context.Context, _ *mcpsdk.CallToolRequest, _ ListProfilesInput,
	) (*mcpsdk.CallToolResult, any, error) {
		summaries, err := profiles.List(ctx)
		if err != nil {
			result, encodeErr := ToolResult(internalErrorDocument(0), true)
			return result, nil, encodeErr
		}
		document := ProfilesDocument{Header: NewHeader(StatusOK, 0), Profiles: summaries}
		if document.Profiles == nil {
			document.Profiles = []ProfileSummary{}
		}
		result, encodeErr := ToolResult(document, false)
		return result, nil, encodeErr
	})
}

func (server *Server) registers(name string) bool {
	return server.classes == nil || server.classes[toolClasses[name]]
}

func registerTool[Input profileSelector](
	server *Server,
	name, description string,
	readOnly bool,
	handler func(context.Context, Dependencies, Input) (any, error),
) {
	if !server.registers(name) {
		return
	}
	destructive := name == "commit_changes" || name == "confirm_reconcile" ||
		name == "confirm_refresh_deletions"
	mcpsdk.AddTool[Input, any](server.SDK, &mcpsdk.Tool{
		Name: server.prefix + name, Description: description,
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive},
	}, func(
		ctx context.Context, _ *mcpsdk.CallToolRequest, input Input,
	) (*mcpsdk.CallToolResult, any, error) {
		document, isError := server.callTool(ctx, input.selectedProfile(),
			func(dependencies Dependencies) (any, error) {
				return handler(ctx, dependencies, input)
			})
		result, err := ToolResult(document, isError)
		return result, nil, err
	})
}

// callTool runs one tool against the profile it names and returns its document and whether the
// document reports an error.
func (server *Server) callTool(
	ctx context.Context,
	profile string,
	run func(Dependencies) (any, error),
) (any, bool) {
	dependencies, release, err := server.profiles.Acquire(ctx, profile)
	if err != nil {
		return server.profileErrorDocument(err), true
	}
	defer release()
	dependencies.Hold = func(workerContext context.Context) (*app.Service, func(), error) {
		held, releaseHeld, holdErr := server.profiles.Acquire(workerContext, dependencies.ProfileID)
		if holdErr != nil {
			return nil, nil, holdErr
		}
		return held.Service, releaseHeld, nil
	}
	document, err := runRecovered(dependencies, run)
	revision := serviceRevision(dependencies.Service)
	if err != nil {
		return failureDocument(ctx, err, revision, internalErrorDetail), true
	}
	if failure, ok := document.(ErrorDocument); ok {
		return document, failure.Status == StatusError
	}
	return document, false
}

func runRecovered(
	dependencies Dependencies,
	run func(Dependencies) (any, error),
) (document any, err error) {
	defer func() {
		if recover() != nil {
			document, err = nil, errToolPanicked
		}
	}()
	return run(dependencies)
}

func (server *Server) profileErrorDocument(err error) ErrorDocument {
	hint := ""
	if server.classes != nil {
		hint = " Pass profile with an ID from " + server.prefix + "list_profiles."
	}
	header := NewHeader(StatusError, 0)
	switch {
	case errors.Is(err, ErrProfileRequired):
		return ErrorDocument{
			Header: header, Code: "profile_required",
			Detail: "Several profiles exist." + hint,
		}
	case errors.Is(err, ErrProfileNotFound):
		return ErrorDocument{
			Header: header, Code: "profile_not_found",
			Detail: "No profile matches the profile argument." + hint,
		}
	default:
		detail := "The profile could not be opened."
		var unavailable UnavailableError
		if errors.As(err, &unavailable) {
			detail = "The profile could not be opened: " + unavailable.Reason
		}
		return ErrorDocument{Header: header, Code: "profile_unavailable", Detail: detail}
	}
}

const internalErrorDetail = "The Moneyflow MCP request could not be completed."

func internalErrorDocument(revision uint64) ErrorDocument {
	return ErrorDocument{
		Header: NewHeader(StatusError, revision), Code: "mcp_internal_error",
		Detail: internalErrorDetail,
	}
}

func serverRevision(server *Server) uint64 {
	if server == nil {
		return 0
	}
	return serviceRevision(server.standalone.Service)
}

func serviceRevision(service *app.Service) uint64 {
	if service == nil {
		return 0
	}
	return service.Revision()
}

// singleProfile serves the one profile a standalone server opened.
type singleProfile struct{ dependencies Dependencies }

func (single singleProfile) Acquire(
	_ context.Context,
	profile string,
) (Dependencies, func(), error) {
	if profile != "" && profile != single.dependencies.ProfileID {
		return Dependencies{}, nil, ErrProfileNotFound
	}
	return single.dependencies, func() {}, nil
}

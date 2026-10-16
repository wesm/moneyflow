package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wesm/moneyflow/internal/access"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/diagnostic"
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
//
// The Dependencies that Acquire returns name the acquired profile in ProfileID, and Acquire must
// accept that ID again. Dependencies.Hold re-acquires the profile by it for background work that
// outlives the tool call, such as a refresh or a provider write, and releases it when the work
// stops.
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
	// ErrToolPanicked reports a tool call that panicked. A Profiles implementation that recovers a
	// panic logs it with LogToolPanic and returns this error; the call answers mcp_internal_error.
	ErrToolPanicked = errors.New("MCP tool stopped unexpectedly") //nolint:revive // product name
)

// LogToolPanic records a panic that a tool call recovered as the event tool_call_panicked, with
// the panic text and frames and never argument values. Call it while the panic is being recovered.
// A nil logger discards the record.
func LogToolPanic(ctx context.Context, logger *slog.Logger, value any) {
	if logger == nil {
		return
	}
	logger.LogAttrs(ctx, slog.LevelError, "moneyflow MCP tool call panicked",
		slog.String(LogEventKey, "tool_call_panicked"), slog.String(LogCauseKey, "internal_error"),
		slog.String("panic", diagnostic.PanicText(value)),
		slog.String("frames", diagnostic.PanicFrames()),
	)
}

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
	// Prefix is prepended to every tool name and matches ^[A-Za-z0-9_.-]*$. The longest resulting
	// tool name must not exceed 128 characters, the MCP SDK's limit.
	Prefix string
}

var toolPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]*$`)

// maxToolNameLength is the longest tool name the MCP SDK accepts.
const maxToolNameLength = 128

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
	if longest := len(options.Prefix) + longestToolName(classes); longest > maxToolNameLength {
		return fmt.Errorf("register MCP tools: a prefix of %d characters makes a tool name %d "+
			"characters long; the limit is %d", len(options.Prefix), longest, maxToolNameLength)
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

// longestToolName returns the length of the longest unprefixed name among the tools that classes
// register.
func longestToolName(classes map[access.Class]bool) int {
	longest := 0
	for name, class := range toolClasses {
		if classes[class] {
			longest = max(longest, len(name))
		}
	}
	return longest
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
	}, recoverPanics(func(
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
	}))
}

// recoverPanics answers a panic anywhere in a tool handler with an internal error result. The
// handlers call a host's Profiles, whose panic would otherwise end the host's process, because
// the MCP SDK does not recover panics in tool handlers.
func recoverPanics[Input any](
	handler mcpsdk.ToolHandlerFor[Input, any],
) mcpsdk.ToolHandlerFor[Input, any] {
	return func(
		ctx context.Context, request *mcpsdk.CallToolRequest, input Input,
	) (result *mcpsdk.CallToolResult, output any, err error) {
		defer func() {
			if recover() != nil {
				output = nil
				result, err = ToolResult(internalErrorDocument(0), true)
			}
		}()
		return handler(ctx, request, input)
	}
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
	}, recoverPanics(func(
		ctx context.Context, _ *mcpsdk.CallToolRequest, input Input,
	) (*mcpsdk.CallToolResult, any, error) {
		document, isError := server.callTool(ctx, input.selectedProfile(),
			func(dependencies Dependencies) (any, error) {
				return handler(ctx, dependencies, input)
			})
		result, err := ToolResult(document, isError)
		return result, nil, err
	}))
}

// callTool runs one tool against the profile it names and returns its document and whether the
// document reports an error. A panic in the tool body answers with the profile's revision, which
// recoverPanics cannot know.
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
	document, err := runRecovered(ctx, dependencies, run)
	revision := serviceRevision(dependencies.Service)
	if err != nil {
		return failureDocument(ctx, err, revision, internalErrorDetail), true
	}
	if failure, ok := document.(ErrorDocument); ok {
		return document, failure.Status == StatusError
	}
	return document, false
}

// runRecovered runs a tool body and turns a panic into ErrToolPanicked after logging it through
// the profile's logger.
func runRecovered(
	ctx context.Context,
	dependencies Dependencies,
	run func(Dependencies) (any, error),
) (document any, err error) {
	defer func() {
		if value := recover(); value != nil {
			LogToolPanic(ctx, dependencies.Logger, value)
			document, err = nil, ErrToolPanicked
		}
	}()
	return run(dependencies)
}

func (server *Server) profileErrorDocument(err error) ErrorDocument {
	hint := server.profileHint()
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
	case errors.Is(err, ErrToolPanicked):
		return internalErrorDocument(0)
	default:
		detail := "The profile could not be opened."
		var unavailable UnavailableError
		if errors.As(err, &unavailable) {
			detail = "The profile could not be opened: " + unavailable.Reason
		}
		return ErrorDocument{Header: header, Code: "profile_unavailable", Detail: detail}
	}
}

// profileHint tells the client of a host's tools where profile IDs come from. A standalone server
// serves one profile, so its documents carry no hint.
func (server *Server) profileHint() string {
	if server.classes == nil {
		return ""
	}
	return " Pass profile with an ID from " + server.prefix + "list_profiles."
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

package mcpgrafana

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// SetGrafanaURLParams are the arguments accepted by the set_grafana_url tool.
type SetGrafanaURLParams struct {
	// URL is the only required argument: it is what actually makes the
	// connection usable. A bare host[:port] (e.g. "localhost:3000") is
	// accepted and normalized via normalizeGrafanaURL before validation.
	URL string `json:"url" jsonschema:"required,description=Base URL of the Grafana instance to connect to\\, e.g. https://my-grafana.example.com. A bare host[:port] like localhost:3000 is also accepted and normalized to http://localhost:3000."`

	// Token, if provided, replaces the bearer token (Grafana service account
	// token, or the deprecated API key) used to authenticate with the new
	// URL. Left nil (omitted) rather than "", so an explicitly empty string
	// deliberately switches to unauthenticated requests, distinct from "not
	// specified, leave whatever this session already had".
	Token *string `json:"token,omitempty" jsonschema:"description=Grafana service account token (or API key) to authenticate with the new URL. Omit to make unauthenticated requests against the new URL."`
}

// SetGrafanaURLResult is returned by the set_grafana_url tool on success.
type SetGrafanaURLResult struct {
	// URL is the normalized URL now active for this session, echoed back so
	// the caller can confirm exactly what was applied (e.g. after a
	// schemeless input like "localhost:3000" was normalized).
	URL string `json:"url"`
	// Message is a human-readable confirmation, suitable for surfacing
	// directly to an end user.
	Message string `json:"message"`
}

// NewSetGrafanaURLTool builds the set_grafana_url tool, which lets a caller
// configure (or change) the Grafana instance this session talks to at
// runtime, without restarting the server or setting GRAFANA_URL up front.
// This is the only tool exempt from RequireGrafanaURLMiddleware's guard by
// necessity (nothing could ever configure the server otherwise); every other
// native tool is blocked with a clear error until this has been called (or
// GRAFANA_URL was set at startup).
//
// The override applies only to the calling MCP session (see
// SessionManager.SetGrafanaOverride) — for SSE/HTTP deployments with multiple
// concurrent clients, one session's set_grafana_url call never affects
// another session's connection or credentials. For stdio, where there is
// only ever one client for the life of the process, this is equivalent to a
// process-wide change.
//
// sm and tm must be the same SessionManager/ToolManager passed to
// mcpgrafana.NewToolManager when the server was constructed, so the override
// this stores is visible to RequireGrafanaURLMiddleware and proxied-tool
// discovery (both of which read it via sm).
func NewSetGrafanaURLTool(sm *SessionManager, tm *ToolManager) Tool {
	return MustTool(
		"set_grafana_url",
		"Configure (or change) the Grafana instance this MCP session connects to, and optionally its auth token. Call this first if other Grafana tools report the URL is not configured, or to point this session at a different Grafana instance at runtime. The change applies only to the calling session, not to other concurrently connected clients.",
		func(ctx context.Context, args SetGrafanaURLParams) (SetGrafanaURLResult, error) {
			return handleSetGrafanaURL(ctx, sm, tm, args)
		},
		mcp.WithTitleAnnotation("Set Grafana URL"),
		mcp.WithIdempotentHintAnnotation(true),
		// Read-only from Grafana's point of view: this only updates the MCP
		// session's connection target, so clients that hide write tools (and
		// --disable-write) still expose it. Without an explicit true hint the
		// MCP default is false, which those clients treat as a write.
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	).NotOrgScoped()
}

// handleSetGrafanaURL validates and normalizes args.URL, stores it (with the
// optional token) as the calling session's Grafana connection override, and
// best-effort refreshes proxied-tool discovery (datasource MCP servers
// reached through Grafana, e.g. Tempo) so it reflects the new instance. Native
// Grafana tools need no such refresh: RequireGrafanaURLMiddleware rebuilds
// their config/clients from the override on every call, so they see the new
// instance starting with the very next tool call.
func handleSetGrafanaURL(ctx context.Context, sm *SessionManager, tm *ToolManager, args SetGrafanaURLParams) (SetGrafanaURLResult, error) {
	normalized := normalizeGrafanaURL(args.URL)
	if err := ValidateGrafanaURL(normalized); err != nil {
		return SetGrafanaURLResult{}, fmt.Errorf("invalid url: %w", err)
	}

	var token string
	if args.Token != nil {
		token = *args.Token
	}

	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return SetGrafanaURLResult{}, fmt.Errorf("no active MCP session found in context; set_grafana_url must be called within a session")
	}
	if sm == nil {
		return SetGrafanaURLResult{}, fmt.Errorf("session management is not available; this server was not constructed with a SessionManager")
	}
	sessionID := session.SessionID()
	sm.SetGrafanaOverride(sessionID, GrafanaOverride{URL: normalized, Token: token})

	if tm != nil {
		refreshProxiedToolsAfterOverride(ctx, tm, sessionID, normalized, token)
	}

	return SetGrafanaURLResult{
		URL:     normalized,
		Message: fmt.Sprintf("Grafana URL configured. This session is now connected to %s.", normalized),
	}, nil
}

// refreshProxiedToolsAfterOverride re-runs proxied-tool discovery (datasource
// MCP servers reached through Grafana) so it reflects the session's new
// override, logging (rather than failing the whole call) if discovery itself
// fails: the native-tool override this call already applied is more
// important than any datasource passthrough tools, and a transient discovery
// failure here (e.g. the new instance is briefly unreachable) shouldn't make
// set_grafana_url itself look like it failed.
func refreshProxiedToolsAfterOverride(ctx context.Context, tm *ToolManager, sessionID, url, token string) {
	logger := GrafanaConfigFromContext(ctx).LoggerOrDefault()

	if !tm.serverMode {
		// HTTP/SSE: OnBeforeListTools/OnBeforeCallTool will re-attach (and
		// therefore re-discover, under the new credential-keyed set) on this
		// session's next hook invocation.
		tm.ResetProxiedToolsForSession(sessionID)
		return
	}

	// stdio: there is no later hook to re-run discovery (see
	// ComposedStdioContextFunc's doc comment), so build the config/client
	// here and re-run it directly.
	config := GrafanaConfigFromContext(ctx)
	config.URL = url
	config.APIKey = token
	newCtx := WithGrafanaConfig(ctx, config)
	newCtx = WithGrafanaClient(newCtx, NewGrafanaClient(newCtx, config.URL, config.APIKey, config.BasicAuth))

	if err := tm.ResetServerProxiedTools(newCtx); err != nil {
		logger.Warn("failed to refresh proxied (datasource) tools after set_grafana_url; native Grafana tools are unaffected", "error", err)
	}
}

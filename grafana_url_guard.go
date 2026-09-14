package mcpgrafana

import (
	"context"
	"fmt"

	"github.com/grafana/incident-go"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// grafanaURLNotConfiguredMessage is returned as a tool-result error (not a
// protocol error, so agents see it and can react) by RequireGrafanaURLMiddleware
// when a Grafana-dependent tool is called before a Grafana URL has been
// configured, either via GRAFANA_URL (or request-header-derived config) at
// startup, or via a runtime call to the set_grafana_url tool.
const grafanaURLNotConfiguredMessage = "Grafana URL is not configured. Please call the `set_grafana_url` tool first."

// grafanaURLExemptTools lists tool names that must remain callable before a
// Grafana URL is configured: set_grafana_url itself (obviously — otherwise
// nothing could ever configure the server), and every tool that never reaches
// a Grafana instance at all (documentation lookups, local config/example
// generation). These match the tools marked .NotOrgScoped() for the same
// underlying reason, plus set_grafana_url.
var grafanaURLExemptTools = map[string]bool{
	"set_grafana_url":                 true,
	"search_docs":                     true,
	"get_doc":                         true,
	"suggest_loki_alloy_label_config": true,
	"get_query_examples":              true,
}

// RequireGrafanaURLMiddleware returns tool-handler middleware that:
//
//  1. Applies the calling session's runtime Grafana connection override, if
//     one was set via the set_grafana_url tool (see
//     SessionManager.SetGrafanaOverride) — rebuilding the GrafanaConfig and
//     Grafana/Incident/Kubernetes clients for the duration of this call only.
//     This rebuild-per-call is necessary (rather than, say, mutating the
//     context set up at connection start) because for stdio transport the
//     context-function chain (ComposedStdioContextFunc) runs exactly once,
//     when server.StdioServer.Listen starts, and that same context is reused
//     for every subsequent message for the lifetime of the process — there is
//     no later point at which a context value could be swapped out for
//     future calls. Applying the override here instead, inside a
//     server.ToolHandlerMiddleware — which mcp-go invokes fresh for every
//     "tools/call" request on every transport — sidesteps that entirely.
//  2. Blocks the call with a clear, actionable error if, after step 1, no
//     Grafana URL is configured for this call — except for a small allowlist
//     of tools that never reach a Grafana instance at all
//     (grafanaURLExemptTools).
//
// This is the single guardrail covering every native tool registered via
// mcpgrafana.Tool.Register (i.e. everything under tools/*.go): it is wired in
// once via server.WithToolHandlerMiddleware, so no individual tool handler
// needs its own "is Grafana configured" check. Proxied tools (datasource MCP
// servers reached through Grafana) are handled separately: they are simply
// never discovered/registered until a Grafana URL is configured, so they
// cannot be called in the first place — see
// ToolManager.InitializeAndRegisterServerTools and
// ToolManager.InitializeAndRegisterProxiedTools.
//
// sm may be nil (no SessionManager available, e.g. a minimal embedder that
// never registers the set_grafana_url tool); step 1 is then skipped entirely,
// and only the connection's startup/env-derived configuration ever applies.
// cache may also be nil; overridden clients are then built fresh on every
// call for that session rather than reused, which is correct but slower.
func RequireGrafanaURLMiddleware(sm *SessionManager, cache *ClientCache) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx = applySessionGrafanaOverride(ctx, sm, cache)

			if !grafanaURLExemptTools[request.Params.Name] && !GrafanaConfigFromContext(ctx).IsConfigured() {
				return mcp.NewToolResultError(grafanaURLNotConfiguredMessage), nil
			}
			return next(ctx, request)
		}
	}
}

// applySessionGrafanaOverride rebuilds ctx's GrafanaConfig and Grafana/
// Incident/Kubernetes clients from the calling session's runtime override, if
// one was set via the set_grafana_url tool. It returns ctx unchanged (a
// no-op) when sm is nil, there is no ClientSession in ctx, or the session has
// no override — in all of those cases the connection's original env/header-
// derived configuration and clients already in ctx apply as-is.
//
// Only URL and APIKey are replaced; every other GrafanaConfig field (OrgID,
// BasicAuth, TLSConfig, extra headers, debug, ...) is carried over from the
// connection's ambient config, so an override only ever changes where/who the
// server talks to, not the rest of the connection's behavior.
func applySessionGrafanaOverride(ctx context.Context, sm *SessionManager, cache *ClientCache) context.Context {
	if sm == nil {
		return ctx
	}
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return ctx
	}
	override, ok := sm.GrafanaOverrideForSession(session.SessionID())
	if !ok {
		return ctx
	}

	config := GrafanaConfigFromContext(ctx)
	config.URL = override.URL
	config.APIKey = override.Token
	ctx = WithGrafanaConfig(ctx, config)
	logger := config.LoggerOrDefault()

	buildGrafana := func() *GrafanaClient {
		return NewGrafanaClient(ctx, config.URL, config.APIKey, config.BasicAuth)
	}
	buildIncident := func() *incident.Client {
		incidentURL := fmt.Sprintf("%s/api/plugins/grafana-irm-app/resources/api/v1/", config.URL)
		client := incident.NewClient(incidentURL, config.APIKey)
		if transport, ok := config.clientTransport(nil, WithoutAuth()); ok {
			client.HTTPClient.Transport = transport
		}
		return client
	}
	buildK8s := func() *KubernetesClient {
		k8sClient, err := NewKubernetesClient(ctx)
		if err != nil {
			logger.Warn("Failed to create Kubernetes client for session Grafana override; k8s APIs will be unavailable for this call", "error", err)
			return nil
		}
		return k8sClient
	}

	if cache == nil {
		ctx = WithGrafanaClient(ctx, buildGrafana())
		ctx = WithIncidentClient(ctx, buildIncident())
		ctx = WithKubernetesClient(ctx, buildK8s())
		return ctx
	}

	// Reuse cached clients across calls that share the same override, same as
	// extractGrafanaClientCached et al. do for header-derived credentials.
	key := cacheKeyFromRequest(config.URL, config.APIKey, config.BasicAuth, config.OrgID, nil)
	ctx = WithGrafanaClient(ctx, cache.GetOrCreateGrafanaClient(key, buildGrafana))
	ctx = WithIncidentClient(ctx, cache.GetOrCreateIncidentClient(key, buildIncident))
	ctx = WithKubernetesClient(ctx, cache.GetOrCreateK8sClient(key, buildK8s))
	return ctx
}

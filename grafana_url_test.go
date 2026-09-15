package mcpgrafana

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGrafanaURLTestServer builds a minimal SessionManager/ToolManager/MCPServer
// trio for exercising set_grafana_url and RequireGrafanaURLMiddleware without
// any real Grafana instance or proxied-tool discovery. Proxied tools are
// disabled so refreshProxiedToolsAfterOverride's ToolManager calls are no-ops.
func newGrafanaURLTestServer(t *testing.T) (*SessionManager, *ToolManager, *server.MCPServer) {
	t.Helper()
	sm := NewSessionManager(WithSessionTTL(0))
	t.Cleanup(sm.Close)
	srv := server.NewMCPServer("test", "1.0")
	tm := NewToolManager(sm, srv, WithProxiedTools(false))
	return sm, tm, srv
}

func TestSetGrafanaURLToolIsReadOnly(t *testing.T) {
	sm, tm, _ := newGrafanaURLTestServer(t)
	tool := NewSetGrafanaURLTool(sm, tm)

	ann := tool.Tool.Annotations
	require.NotNil(t, ann.ReadOnlyHint, "readOnlyHint must be set so clients do not treat this as a write tool")
	assert.True(t, *ann.ReadOnlyHint)
	require.NotNil(t, ann.DestructiveHint)
	assert.False(t, *ann.DestructiveHint)
	require.NotNil(t, ann.OpenWorldHint)
	assert.False(t, *ann.OpenWorldHint)
	require.NotNil(t, ann.IdempotentHint)
	assert.True(t, *ann.IdempotentHint)
}

func TestSessionManagerGrafanaOverride(t *testing.T) {
	t.Run("no override by default", func(t *testing.T) {
		sm, _, _ := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)

		_, ok := sm.GrafanaOverrideForSession("s1")
		assert.False(t, ok)
	})

	t.Run("set then get round-trips", func(t *testing.T) {
		sm, _, _ := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)

		sm.SetGrafanaOverride("s1", GrafanaOverride{URL: "https://grafana.example.com", Token: "tok"})

		override, ok := sm.GrafanaOverrideForSession("s1")
		require.True(t, ok)
		assert.Equal(t, "https://grafana.example.com", override.URL)
		assert.Equal(t, "tok", override.Token)
	})

	t.Run("overwriting replaces the previous override", func(t *testing.T) {
		sm, _, _ := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)

		sm.SetGrafanaOverride("s1", GrafanaOverride{URL: "https://first.example.com", Token: "a"})
		sm.SetGrafanaOverride("s1", GrafanaOverride{URL: "https://second.example.com", Token: "b"})

		override, ok := sm.GrafanaOverrideForSession("s1")
		require.True(t, ok)
		assert.Equal(t, "https://second.example.com", override.URL)
		assert.Equal(t, "b", override.Token)
	})

	t.Run("untracked session is a no-op, not a panic", func(t *testing.T) {
		sm, _, _ := newGrafanaURLTestServer(t)

		assert.NotPanics(t, func() {
			sm.SetGrafanaOverride("never-created", GrafanaOverride{URL: "https://grafana.example.com"})
		})
		_, ok := sm.GrafanaOverrideForSession("never-created")
		assert.False(t, ok)
	})

	t.Run("different sessions have independent overrides", func(t *testing.T) {
		sm, _, _ := newGrafanaURLTestServer(t)
		sessA := &mockClientSession{id: "a"}
		sessB := &mockClientSession{id: "b"}
		sm.CreateSession(context.Background(), sessA)
		sm.CreateSession(context.Background(), sessB)

		sm.SetGrafanaOverride("a", GrafanaOverride{URL: "https://a.example.com"})

		_, bHasOverride := sm.GrafanaOverrideForSession("b")
		assert.False(t, bHasOverride, "session b must not see session a's override")

		aOverride, aOk := sm.GrafanaOverrideForSession("a")
		require.True(t, aOk)
		assert.Equal(t, "https://a.example.com", aOverride.URL)
	})
}

func TestHandleSetGrafanaURL(t *testing.T) {
	t.Run("valid absolute URL is stored verbatim", func(t *testing.T) {
		sm, tm, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)
		ctx := srv.WithContext(context.Background(), sess)

		result, err := handleSetGrafanaURL(ctx, sm, tm, SetGrafanaURLParams{URL: "https://grafana.example.com"})
		require.NoError(t, err)
		assert.Equal(t, "https://grafana.example.com", result.URL)
		assert.Contains(t, result.Message, "https://grafana.example.com")

		override, ok := sm.GrafanaOverrideForSession("s1")
		require.True(t, ok)
		assert.Equal(t, "https://grafana.example.com", override.URL)
		assert.Equal(t, "", override.Token)
	})

	t.Run("schemeless host is normalized before storing", func(t *testing.T) {
		sm, tm, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)
		ctx := srv.WithContext(context.Background(), sess)

		result, err := handleSetGrafanaURL(ctx, sm, tm, SetGrafanaURLParams{URL: "localhost:3000"})
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:3000", result.URL)
	})

	t.Run("trailing slash is trimmed", func(t *testing.T) {
		sm, tm, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)
		ctx := srv.WithContext(context.Background(), sess)

		result, err := handleSetGrafanaURL(ctx, sm, tm, SetGrafanaURLParams{URL: "https://grafana.example.com/"})
		require.NoError(t, err)
		assert.Equal(t, "https://grafana.example.com", result.URL)
	})

	t.Run("token is stored when provided", func(t *testing.T) {
		sm, tm, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)
		ctx := srv.WithContext(context.Background(), sess)

		token := "my-service-account-token"
		_, err := handleSetGrafanaURL(ctx, sm, tm, SetGrafanaURLParams{URL: "https://grafana.example.com", Token: &token})
		require.NoError(t, err)

		override, ok := sm.GrafanaOverrideForSession("s1")
		require.True(t, ok)
		assert.Equal(t, token, override.Token)
	})

	t.Run("omitted token leaves the override's token empty", func(t *testing.T) {
		sm, tm, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)
		ctx := srv.WithContext(context.Background(), sess)

		_, err := handleSetGrafanaURL(ctx, sm, tm, SetGrafanaURLParams{URL: "https://grafana.example.com"})
		require.NoError(t, err)

		override, ok := sm.GrafanaOverrideForSession("s1")
		require.True(t, ok)
		assert.Equal(t, "", override.Token)
	})

	t.Run("invalid URL is rejected and no override is stored", func(t *testing.T) {
		sm, tm, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)
		ctx := srv.WithContext(context.Background(), sess)

		_, err := handleSetGrafanaURL(ctx, sm, tm, SetGrafanaURLParams{URL: "not a url"})
		require.Error(t, err)

		_, ok := sm.GrafanaOverrideForSession("s1")
		assert.False(t, ok, "a rejected URL must not be stored as an override")
	})

	t.Run("empty URL is rejected", func(t *testing.T) {
		sm, tm, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)
		ctx := srv.WithContext(context.Background(), sess)

		_, err := handleSetGrafanaURL(ctx, sm, tm, SetGrafanaURLParams{URL: ""})
		assert.Error(t, err)
	})

	t.Run("non-http(s) scheme is rejected", func(t *testing.T) {
		sm, tm, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)
		ctx := srv.WithContext(context.Background(), sess)

		_, err := handleSetGrafanaURL(ctx, sm, tm, SetGrafanaURLParams{URL: "ftp://grafana.example.com"})
		assert.Error(t, err)
	})

	t.Run("no session in context is an error", func(t *testing.T) {
		sm, tm, _ := newGrafanaURLTestServer(t)

		_, err := handleSetGrafanaURL(context.Background(), sm, tm, SetGrafanaURLParams{URL: "https://grafana.example.com"})
		assert.Error(t, err)
	})

	t.Run("nil SessionManager is an error", func(t *testing.T) {
		_, tm, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		ctx := srv.WithContext(context.Background(), sess)

		_, err := handleSetGrafanaURL(ctx, nil, tm, SetGrafanaURLParams{URL: "https://grafana.example.com"})
		assert.Error(t, err)
	})
}

func TestRequireGrafanaURLMiddleware(t *testing.T) {
	callToolRequest := func(name string) mcp.CallToolRequest {
		req := mcp.CallToolRequest{}
		req.Params.Name = name
		return req
	}

	nextCalled := func() (server.ToolHandlerFunc, *bool) {
		called := false
		return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			called = true
			return mcp.NewToolResultText("ok"), nil
		}, &called
	}

	t.Run("blocks a non-exempt tool when unconfigured", func(t *testing.T) {
		next, called := nextCalled()
		handler := RequireGrafanaURLMiddleware(nil, nil)(next)

		ctx := WithGrafanaConfig(context.Background(), GrafanaConfig{})
		result, err := handler(ctx, callToolRequest("search_dashboards"))
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
		text, ok := result.Content[0].(mcp.TextContent)
		require.True(t, ok)
		assert.Equal(t, grafanaURLNotConfiguredMessage, text.Text)
		assert.False(t, *called, "the wrapped handler must not run when unconfigured")
	})

	t.Run("allows an exempt tool when unconfigured", func(t *testing.T) {
		for _, name := range []string{"set_grafana_url", "search_docs", "get_doc", "suggest_loki_alloy_label_config", "get_query_examples"} {
			t.Run(name, func(t *testing.T) {
				next, called := nextCalled()
				handler := RequireGrafanaURLMiddleware(nil, nil)(next)

				ctx := WithGrafanaConfig(context.Background(), GrafanaConfig{})
				result, err := handler(ctx, callToolRequest(name))
				require.NoError(t, err)
				assert.False(t, result.IsError)
				assert.True(t, *called, "exempt tools must always reach the wrapped handler")
			})
		}
	})

	t.Run("allows a non-exempt tool when configured via GrafanaConfig", func(t *testing.T) {
		next, called := nextCalled()
		handler := RequireGrafanaURLMiddleware(nil, nil)(next)

		ctx := WithGrafanaConfig(context.Background(), GrafanaConfig{URL: "https://grafana.example.com"})
		result, err := handler(ctx, callToolRequest("search_dashboards"))
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.True(t, *called)
	})

	t.Run("session override unblocks a previously-unconfigured connection", func(t *testing.T) {
		sm, _, srv := newGrafanaURLTestServer(t)
		sess := &mockClientSession{id: "s1"}
		sm.CreateSession(context.Background(), sess)
		// A loopback address on a port nothing listens on: NewGrafanaClient's
		// best-effort frontend-settings fetch fails fast (connection refused)
		// rather than depending on, or waiting out a timeout against, a real
		// network destination.
		sm.SetGrafanaOverride("s1", GrafanaOverride{URL: "http://127.0.0.1:1", Token: "tok"})

		var seenConfig GrafanaConfig
		next := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			seenConfig = GrafanaConfigFromContext(ctx)
			return mcp.NewToolResultText("ok"), nil
		}
		handler := RequireGrafanaURLMiddleware(sm, nil)(next)

		// The connection itself has no Grafana URL configured...
		ctx := WithGrafanaConfig(context.Background(), GrafanaConfig{})
		ctx = srv.WithContext(ctx, sess)

		result, err := handler(ctx, callToolRequest("search_dashboards"))
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError, "the session's override must unblock the call")
		assert.Equal(t, "http://127.0.0.1:1", seenConfig.URL)
		assert.Equal(t, "tok", seenConfig.APIKey)
	})

	t.Run("session without an override is unaffected by another session's override", func(t *testing.T) {
		sm, _, srv := newGrafanaURLTestServer(t)
		sessA := &mockClientSession{id: "a"}
		sessB := &mockClientSession{id: "b"}
		sm.CreateSession(context.Background(), sessA)
		sm.CreateSession(context.Background(), sessB)
		sm.SetGrafanaOverride("a", GrafanaOverride{URL: "https://a.example.com"})

		next, called := nextCalled()
		handler := RequireGrafanaURLMiddleware(sm, nil)(next)

		ctx := WithGrafanaConfig(context.Background(), GrafanaConfig{}) // connection-level: unconfigured
		ctx = srv.WithContext(ctx, sessB)

		result, err := handler(ctx, callToolRequest("search_dashboards"))
		require.NoError(t, err)
		assert.True(t, result.IsError, "session b has no override and the connection is unconfigured")
		assert.False(t, *called)
	})
}

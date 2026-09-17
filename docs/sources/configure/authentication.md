---
title: Authentication
menuTitle: Authentication
description: Authenticate the Grafana MCP server to Grafana using a service account token or username and password.
keywords:
  - authentication
  - service account
  - token
  - MCP
weight: 1
aliases:
  - /docs/grafana-cloud/machine-learning/mcp/configure/authentication/
---

# Authentication

The Grafana MCP server needs a URL (and usually credentials) to call the Grafana API. Use a service account token (recommended) or a username and password.

`GRAFANA_URL` and its credentials are optional at startup: the server starts successfully without them, and every tool that needs Grafana returns a clear error until a URL is configured. Set them via environment variables up front, or call the `set_grafana_url` tool at runtime — see [Configure at runtime](#configure-at-runtime) below.

## What you'll achieve

You set the environment variables (or equivalent) so the server can authenticate to your Grafana instance. The same credentials apply whether you run the server with uvx, Docker, a binary, or Helm.

## Before you begin

- A Grafana instance (Grafana 9.0 or later).
- For token auth: a [service account](https://grafana.com/docs/grafana/latest/administration/service-accounts/#add-a-token-to-a-service-account-in-grafana) with a token and the RBAC permissions needed for the tools you use.

## Use a service account token

Set `GRAFANA_URL` and `GRAFANA_SERVICE_ACCOUNT_TOKEN` in the environment passed to the server.

- **GRAFANA_URL:** Your Grafana base URL (for example, `http://localhost:3000` or `https://myinstance.grafana.net`).
- **GRAFANA_SERVICE_ACCOUNT_TOKEN:** The token you created for the service account.

The deprecated `GRAFANA_API_KEY` is still supported but will be removed in a future version; use `GRAFANA_SERVICE_ACCOUNT_TOKEN` instead.

### Read the token from a file

Set `GRAFANA_SERVICE_ACCOUNT_TOKEN_FILE` to a file path that contains the token instead of passing the value inline. The file is read fresh on every request, so a rotated token is picked up automatically without restarting the server.

This is useful in Kubernetes, where a Secret mounted as a volume is updated in place when the underlying Secret changes. Because the server's client cache is keyed on the token value, a rotated token transparently produces a new client with no pod restart:

```yaml
env:
  - name: GRAFANA_SERVICE_ACCOUNT_TOKEN_FILE
    value: /var/run/secrets/grafana/token
volumeMounts:
  - name: grafana-token
    mountPath: /var/run/secrets/grafana
    readOnly: true
volumes:
  - name: grafana-token
    secret:
      secretName: grafana-mcp-token
```

Surrounding whitespace (including a trailing newline) is trimmed from the file contents. If both `GRAFANA_SERVICE_ACCOUNT_TOKEN` and `GRAFANA_SERVICE_ACCOUNT_TOKEN_FILE` are set, the inline token takes precedence.

## Use username and password

You can use basic auth by setting `GRAFANA_USERNAME` and `GRAFANA_PASSWORD` instead of a token. This is less suitable for automation; prefer a service account token when possible.

## Configure at runtime

If `GRAFANA_URL` is not set (or you want to point the server at a different instance), call the `set_grafana_url` tool from your MCP client:

- **url** (required): the Grafana base URL, for example `https://myinstance.grafana.net`. A bare `host[:port]` like `localhost:3000` is also accepted and normalized to `http://localhost:3000`.
- **token** (optional): a service account token (or the deprecated API key) to authenticate with. Omit it to make unauthenticated requests against the new URL.

`set_grafana_url` applies only to the calling MCP session — for SSE/streamable-http deployments with multiple concurrent clients, one session's call never affects another session's connection or credentials. For stdio, where there is only ever one client for the life of the process, this is equivalent to a process-wide change.

Until a URL is configured (via `GRAFANA_URL`/`GRAFANA_SERVICE_ACCOUNT_TOKEN` or `set_grafana_url`), every Grafana-dependent tool returns:

```text
Grafana URL is not configured. Please call the `set_grafana_url` tool first.
```

A small set of tools that don't need a Grafana connection (for example, docs search) remain usable even when unconfigured.

## Next steps

- [Enable and disable tools](../enable-and-disable-tools/) to control which MCP tools are available.
- [Transports and addresses](../transports-and-addresses/) for stdio, SSE, and streamable-http.

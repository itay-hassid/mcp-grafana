---
title: Cursor
menuTitle: Cursor
description: Set up the Grafana MCP server for Cursor.
keywords:
  - Cursor
  - MCP
  - client
weight: 2
aliases:
  - /docs/grafana-cloud/machine-learning/mcp/clients/cursor/
---

# Cursor

This guide helps you set up the `mcp-grafana` server for Cursor.

## Prerequisites

- Cursor IDE installed
- Grafana 9.0+ (a service account token is recommended, but not required)
- `mcp-grafana` binary in your PATH

`GRAFANA_URL` and `GRAFANA_SERVICE_ACCOUNT_TOKEN` are optional: the server starts without them, and you can configure (or change) the Grafana connection at runtime by asking the agent to call the `set_grafana_url` tool with a `url` (and optional `token`). See [Configure authentication](../../configure/authentication/#configure-at-runtime) for details. The examples below set them via environment variables for convenience.

## Configuration

Two options for configuration location:

| Scope                 | Path                               |
| :-------------------- | :--------------------------------- |
| Global (all projects) | `~/.cursor/mcp.json`               |
| Project-specific      | `.cursor/mcp.json` in project root |

### Add using the UI

1. Open Cursor Settings -> **Tools & Integrations**
2. Click **New MCP Server**
3. This opens `~/.cursor/mcp.json` for editing

### Manual configuration

Create or edit `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "grafana": {
      "command": "mcp-grafana",
      "args": [],
      "env": {
        "GRAFANA_URL": "http://localhost:3000",
        "GRAFANA_SERVICE_ACCOUNT_TOKEN": "<your-token>"
      }
    }
  }
}
```

### Docker configuration

```json
{
  "mcpServers": {
    "grafana": {
      "command": "docker",
      "args": [
        "run",
        "--rm",
        "-i",
        "-e",
        "GRAFANA_URL",
        "-e",
        "GRAFANA_SERVICE_ACCOUNT_TOKEN",
        "grafana/mcp-grafana",
        "-t",
        "stdio"
      ],
      "env": {
        "GRAFANA_URL": "http://host.docker.internal:3000",
        "GRAFANA_SERVICE_ACCOUNT_TOKEN": "<your-token>"
      }
    }
  }
}
```

## Debug mode

```json
{
  "mcpServers": {
    "grafana": {
      "command": "mcp-grafana",
      "args": ["-debug"],
      "env": {
        "GRAFANA_URL": "http://localhost:3000",
        "GRAFANA_SERVICE_ACCOUNT_TOKEN": "<your-token>"
      }
    }
  }
}
```

## Verify configuration

1. Go to Cursor Settings -> **Tools & Integrations**
2. Find **grafana** in the MCP servers list
3. Click the refresh button if needed
4. Green indicator = server running
5. Open Composer and ask: "List my Grafana dashboards"

## Troubleshooting

**Server not appearing:**

- Check JSON syntax (trailing commas break it)
- Restart Cursor
- Verify binary path: `which mcp-grafana`

**Tools not working:**

- Click refresh button in MCP settings
- Check Grafana token permissions
- Enable `-debug` flag and check output

## Read-only mode

```json
{
  "mcpServers": {
    "grafana": {
      "command": "mcp-grafana",
      "args": ["--disable-write"],
      "env": {
        "GRAFANA_URL": "http://localhost:3000",
        "GRAFANA_SERVICE_ACCOUNT_TOKEN": "<your-token>"
      }
    }
  }
}
```

## Next steps

- [Set up](../../set-up/) for other install options (uvx, Helm).
- [Configure authentication](../../configure/authentication/) for Grafana credentials.

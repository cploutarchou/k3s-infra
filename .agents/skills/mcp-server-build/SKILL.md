---
name: mcp-server-build
description: Use when building or debugging a remote MCP server that claude.ai (or another MCP client) connects to over HTTP — transport choice, auth model, multi-replica behavior, connector "Couldn't connect" failures, and the read-only-tools/writes-via-PR pattern.
---

# Building a remote MCP server for claude.ai

Hard-won rules for an HTTP MCP server that a hosted client (claude.ai
custom connectors) actually connects to.

## Transport

- **Streamable HTTP** at a single endpoint (`/mcp`). Serve a separate
  unauthenticated `/healthz` for probes and uptime checks.
- **Stateless mode** if the server library supports it (e.g. mcp-go's
  `WithStateLess(true)`). Default streamable-HTTP session state lives in
  process memory, so two replicas behind one Service break sessions on
  round-robin. Stateless removes the need for sticky routing and makes
  horizontal scaling and zero-downtime rolls trivial. Read-only tool
  servers lose nothing.
- With ≥2 replicas, add pod anti-affinity across nodes so a node loss
  doesn't take both.

## Auth model (this exact shape, learned the hard way)

- **The connector probes the URL before any credential is configured.** A
  hard 401 on `initialize` makes claude.ai conclude the URL is not a valid
  MCP server ("Couldn't connect"). Allow the handshake subset
  unauthenticated: `initialize`, `notifications/initialized`,
  `tools/list`, `ping`. These expose only the server name and tool
  schemas — no data.
- **Everything else — `tools/call` above all — requires the key.** Return
  401 with a `WWW-Authenticate` header on unauthorized calls.
- **Accept the key as both `X-API-Key: <key>` and
  `Authorization: Bearer <key>`**, compared constant-time
  (`crypto/subtle` or equivalent). Empirical fact from access-log capture:
  claude.ai custom connectors send the configured header (`X-Api-Key`)
  on every request and **never send `Authorization`** — but keep Bearer
  for SDK/API clients, which use `authorization_token`.
- Refuse to start with an empty key. Never log key values — if you must
  log auth headers for debugging, use a proxy log mode that structurally
  redacts values and only records presence.
- Peek the JSON-RPC `method` from the request body to make the
  public/protected decision; treat unparseable or batch bodies as
  protected.

## Tool design

- **Read-only tools** for everything observational: nodes, workloads,
  events, logs (size-capped), controller status, an aggregate health
  report. Bound every response (tail lines, byte caps, list limits).
- **Writes only through an out-of-band review path**: a tool that opens a
  PR against the config repo, and at most a "reconcile now" nudge.
  The server's RBAC should make anything else impossible — get/list/watch
  plus patch on exactly the reconciler kinds, nothing more. The service
  account's permissions are the real enforcement; the tool list is just
  UX.
- Non-resource endpoints (e.g. `/healthz/*` on an API server) need their
  own explicit RBAC grant — plain authenticated service accounts get 403
  there, and your health tool silently reports "unhealthy" otherwise.

## Operational notes

- Expect internet scanners to find the hostname within minutes of TLS
  issuance (Certificate Transparency logs). Register only the routes you
  mean to serve; everything else 404s harmlessly.
- Behind a CDN/proxy, the client's real IP is in the proxy's forwarded
  header (`X-Forwarded-For` or the CDN's connecting-IP header), not the
  TCP peer address — filter or log accordingly when attributing traffic.
- Version the server; report name+version in `initialize` so a connector
  listing identifies the deployment.

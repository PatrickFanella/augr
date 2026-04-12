---
title: "Web UI"
description: "Frontend route map, page behavior, and current limitations for the React operator interface."
status: "canonical"
updated: "2026-04-03"
tags: [frontend, web-ui, reference]
---

# Web UI

The frontend lives in `web/` and is mounted through the route map in `web/src/App.tsx`.

## Route map

| Route | Purpose |
| --- | --- |
| `/login` | login screen |
| `/` | dashboard |
| `/strategies` | strategy list and creation |
| `/strategies/:id` | strategy detail, config editing, lifecycle actions |
| `/runs` | run history |
| `/runs/:id` | run replay/detail |
| `/portfolio` | portfolio analytics and position view |
| `/memories` | memory search and deletion |
| `/settings` | provider/risk/system settings |
| `/risk` | kill switch, circuit breaker, utilization, audit view |
| `/realtime` | live event and conversation surface |
| `/reliability` | automation job health, pipeline failure indicators |

## Auth behavior

- `/login` is behind `PublicOnlyRoute`
- the rest of the application is behind `ProtectedRoute`
- the frontend expects API login to return JWT tokens and then uses them for protected requests

## Page-by-page reference

### Dashboard

Primary components:

- portfolio summary
- active strategies
- activity feed
- risk status bar

Use it for:

- quick “is the system healthy?” checks
- seeing current exposure and recent activity at a glance

### Strategies

Use it for:

- listing strategies
- creating strategies
- launching manual runs

### Strategy detail

Capabilities:

- view strategy metadata
- run now
- pause
- resume
- skip next
- delete
- inspect run history
- edit structured config

Important caveat:

- several strategy-editor-related frontend files currently contain merge conflicts, so API behavior is more trustworthy than the current editor UX until cleanup happens

### Runs

Capabilities:

- filter by strategy
- filter by status
- filter by date range
- paginate through run history
- jump to run detail

### Run detail

Capabilities:

- phase progress view
- analyst cards
- debate view
- trader plan
- final signal view
- decision inspector
- WebSocket-driven refresh while a run is in progress

This is the deepest introspection surface in the current UI.

### Portfolio

Use it for:

- portfolio summary
- open positions
- historical trade context
- visual performance context

### Memories

Use it for:

- full-text search across stored memories
- filtering by agent role
- inspecting a memory record
- deleting stale memories

Current caveat:

- `web/src/pages/memories-page.tsx` currently contains merge-conflict markers, so this page needs cleanup before being treated as stable product truth

### Settings

Capabilities:

- inspect current LLM provider and model selection
- edit provider settings
- edit risk settings
- inspect environment/version/uptime
- inspect broker configuration summaries
- toggle kill switch from the settings surface

Critical caveat:

- settings edits are not durable across restart

### Risk

Capabilities:

- inspect circuit-breaker state
- activate/deactivate kill switch
- supply activation reason
- inspect exposure/utilization bars
- inspect recent audit activity

### Realtime

Intended purpose:

- live event feed
- live conversations or operator-facing realtime context

Current caveat:

- the realtime page and related components currently contain merge-conflict markers; do not treat this route as stable until those are resolved

## Frontend/runtime coupling

The frontend depends on:

- the REST API for initial/query state
- WebSocket updates for realtime refresh on active runs and the terminal dashboard path
- current API route shapes and pagination semantics

If UI behavior feels inconsistent, verify the backing route in [API](api.md) before assuming the frontend is correct.

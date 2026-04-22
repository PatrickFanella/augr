# Hermes integration proposal for augr

## Goal

Integrate Hermes with the existing `~/.agents` hub and the `augr` repo in a way that is low-risk, operationally useful, and consistent with the hub's documented boundary:

- shared, human-managed assets live in `~/.agents`
- mutable runtime state stays native to each tool
- augr consumes the hub for workspace bootstrapping rather than inventing a parallel agent layout

## What I found

### In `augr`

- The repo already expects the shared agent hub and documents a `~/.agents`-based workflow in `README.md`.
- `Taskfile.yml` defines:
  - `task workspace`
  - `task workspace:research`
  - `task workspace:review`
  - `task workspace:ops`
- But those tasks call `./scripts/workspace.sh`, and that file is missing.
- The actual hub workspace launcher exists in `~/.agents/scripts/bootstrap_tmux_workspace.sh`.
- The augr backend already exposes the right operator-facing surfaces for Hermes:
  - `GET /api/v1/runs/{id}`
  - `GET /api/v1/runs/{id}/decisions`
  - `GET /api/v1/runs/{id}/snapshot`
  - `GET /api/v1/events`
  - `GET /api/v1/memories`
  - `POST /api/v1/memories/search`
  - `GET/POST /api/v1/conversations`
  - `GET/POST /api/v1/conversations/{id}/messages`
- `internal/service/conversation.go` already builds LLM context from decisions, snapshots, and memories, which means augr already has a good "ask the agent why it did that" surface.

### In `~/.agents`

- The hub has a clear pattern for adding managed harnesses via `agents/<name>/agent.yaml`.
- The documented strategies are:
  - `symlink-subpaths`
  - `template-or-copy`
  - `docs-only`
  - `native-only`
- The hub script currently opens tmux windows for:
  - `edit`
  - `deck`
  - `claude`
  - `opencode`
  - `db`
  - `ops`
- Agent metadata is validated by schema and hub doctor tooling.
- The hub explicitly warns against centralizing mutable runtime state.

## Recommendation

Use a two-layer integration:

1. `~/.agents` integration for developer workflow
2. augr API integration for operator workflow

Do **not** put Hermes directly into augr's trading runtime first.

That would be the wrong first move because it touches execution-critical code (`internal/agent`, runner wiring, risk flow, provider routing) when augr already exposes a safer control-plane interface for investigation and operations.

## Proposed architecture

### Layer 1: Add Hermes as a managed harness in `~/.agents`

Add a new registry entry:

- `~/.agents/agents/hermes/agent.yaml`

Recommended initial posture:

- `hub_strategy: native-only` or `docs-only`
- keep Hermes runtime state outside the hub
- use the hub only for:
  - docs
  - prompts
  - launcher integration
  - discoverability in hub doctor / bootstrap reporting

Why this posture:

- it matches the hub rule: centralize durable assets, not runtime state
- it avoids guessing at Hermes config semantics too early
- it gets Hermes into the standard tooling surface immediately

Suggested initial metadata shape:

```yaml
name: hermes
cli: hermes
config: ~/.hermes
runtime_roots:
  - ~/.hermes
  - ~/Documents/hermes
config_link: configs/hermes
binary: hermes
binary_type: cli
description: Hermes CLI agent and automation runtime
install_hint: Install Hermes CLI and ensure `hermes` is on PATH.
maturity: experimental
supports_mcp: false
optional: true
hub_strategy: native-only
config_required: true
shared_assets:
  - docs
  - prompts
bootstrap:
  check_paths:
    - ~/.hermes
    - ~/Documents/hermes
  notes:
    - Keep Hermes runtime state native; do not centralize sessions, caches, or credentials in the hub.
validated_platforms:
  - linux
```

Notes:

- I would start with `supports_mcp: false` unless Hermes has a documented MCP contract you want the hub to validate.
- If Hermes later grows a stable rendered-config surface, you can move from `native-only` to `template-or-copy`.

### Layer 2: Make augr launch Hermes from the shared workspace

Fix the current broken workspace path in augr.

Current problem:

- `Taskfile.yml` points to `./scripts/workspace.sh`
- that file does not exist

Proposed fix:

- add `Code/projects/augr/scripts/workspace.sh` as a thin wrapper over the hub launcher
- wrapper should call:

```bash
~/.agents/scripts/bootstrap_tmux_workspace.sh "$(pwd)" "${1:-$(basename "$(pwd)")}" 
```

Then extend the shared hub launcher to support Hermes as an optional extra window:

- add env vars like:
  - `HERMES_BIN=${HERMES_BIN:-hermes}`
  - `ENABLE_HERMES_WINDOW=${ENABLE_HERMES_WINDOW:-1}`
- if Hermes is installed, open a `hermes` tmux window after `opencode`

Result:

- augr keeps using the shared hub model
- Hermes becomes part of the standard repo workspace
- no augr-specific hardcoding of Hermes internals is needed

## How Hermes should talk to augr

Hermes should integrate with augr as an operator/control-plane client first, not as a runtime trading role.

### Phase A: operator assistant over augr APIs

Create a small Hermes-side augr integration surface that can:

- authenticate with augr using API key or login flow
- list and inspect strategy runs
- fetch run decisions and snapshots
- search memories
- read events
- open or continue agent conversations
- trigger manual actions:
  - run strategy
  - cancel run
  - inspect risk status
  - toggle kill switch only with explicit operator confirmation

This can be delivered as either:

1. a Hermes skill/documented workflow, or
2. a lightweight augr client script/CLI wrapper consumed by Hermes

I recommend starting with a thin client wrapper because augr already has stable HTTP surfaces.

### Minimum useful endpoints for Hermes

Read-only:

- `GET /api/v1/strategies`
- `GET /api/v1/runs`
- `GET /api/v1/runs/{id}`
- `GET /api/v1/runs/{id}/decisions`
- `GET /api/v1/runs/{id}/snapshot`
- `GET /api/v1/events`
- `GET /api/v1/memories`
- `POST /api/v1/memories/search`
- `GET /api/v1/conversations`
- `GET /api/v1/conversations/{id}/messages`

Write actions:

- `POST /api/v1/conversations`
- `POST /api/v1/conversations/{id}/messages`
- `POST /api/v1/strategies/{id}/run`
- `POST /api/v1/runs/{id}/cancel`
- `POST /api/v1/risk/killswitch`

### Why this is the right first integration

Because augr already stores and exposes the exact artifacts Hermes needs to be useful:

- decisions
- snapshots
- events
- memories
- conversations

That means Hermes can act as:

- operator copilot
- run investigator
- postmortem assistant
- risk review assistant

without changing augr's execution pipeline.

## Repo changes I would make first

### In `~/.agents`

1. Add `agents/hermes/agent.yaml`
2. Add `docs/HERMES.md` or similar usage doc
3. Update `scripts/bootstrap_tmux_workspace.sh` to optionally open a `hermes` window
4. Update validation/docs if needed

### In `augr`

1. Add `scripts/workspace.sh` wrapper so existing `task workspace*` commands actually work
2. Add `docs/hermes-integration.md` with:
   - auth setup
   - common Hermes workflows
   - safe actions vs dangerous actions
3. Optionally add a small helper script, e.g.:
   - `scripts/hermes-augr.sh`
   - or `scripts/hermes_api.py`

### Optional UI addition later

In augr web UI, add a "Open in Hermes" affordance from:

- run detail page
- decision timeline
- conversation page

That action could:

- copy a run-focused prompt
- open a local URL/command bridge if you later build one
- or simply render a ready-to-paste Hermes command block

## Concrete first milestone

If I were implementing this in the least risky order, I would do:

1. Fix augr workspace launcher by adding `scripts/workspace.sh`
2. Register Hermes in `~/.agents/agents/hermes/agent.yaml`
3. Extend the hub tmux launcher with an optional `hermes` window
4. Add an augr helper script for read-only inspection:
   - get run
   - get decisions
   - get snapshot
   - search memories
   - list conversations
5. Add a short repo doc showing example Hermes operator workflows

That gives immediate value with minimal blast radius.

## What I would not do first

I would not start by:

- adding Hermes as a new trading/runtime role inside `internal/agent`
- replacing augr's LLM provider layer with Hermes
- routing execution decisions through Hermes
- storing Hermes runtime state inside `~/.agents`

Those are higher-risk and solve the wrong problem first.

## Validation

### Hub validation

After adding Hermes to `~/.agents`:

```bash
cd ~/.agents
python3 scripts/validate_agent_yaml.py
python3 scripts/validate_hub_metadata.py
python3 scripts/hub_doctor.py
bash scripts/validate-hub.sh
```

### augr validation

After adding the launcher wrapper:

```bash
cd ~/Code/projects/augr
task workspace
```

Expected result:

- tmux session opens successfully
- standard windows appear
- Hermes window appears when enabled and installed

### augr API integration validation

Verify Hermes-side read-only flows against:

- runs
- decisions
- snapshot
- memories search
- conversations

before enabling write actions.

## Final recommendation

Best path:

- integrate Hermes into `~/.agents` as a first-class harness
- fix augr to use the shared hub launcher it already expects
- use augr's existing API/conversation/memory surfaces so Hermes acts as an operator assistant first
- defer deep runtime integration until the workflow proves valuable

This gives you a practical Hermes-on-augr workflow quickly, while staying aligned with both augr's current architecture and the hub's documented boundaries.

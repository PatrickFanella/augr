# n8n integration guide

The runtime exposes a dedicated n8n notification surface backed by the structured webhook notifier.
The canonical variables are:

- `N8N_WEBHOOK_URL`
- `N8N_WEBHOOK_SECRET`

The n8n channel name is `n8n`.
Use that channel in any `ALERT_*_CHANNELS` variable when you want alerts forwarded to n8n.

## What the runtime can send to n8n

| Event type | Sent today | Trigger path | Caveats |
| --- | --- | --- | --- |
| `alert` | yes | alert manager -> `n8n` channel | only if the relevant `ALERT_*_CHANNELS` entry includes `n8n` |
| `signal` | yes | smoke strategy runner -> `RecordSignal` | current runtime only wires manual runs in `APP_ENV=smoke` |
| `decision` | yes | smoke strategy runner -> `RecordDecision` | same smoke/manual-run caveat |

## n8n setup

1. In n8n, create a new workflow.
2. Add a **Webhook** trigger node.
3. Set the HTTP method you want to accept. `POST` is the current runtime behavior.
4. During testing, click **Listen for Test Event** and use the **Test URL**.
5. Before using the integration for real traffic, activate the workflow and copy the **Production URL**.
6. Put the Production URL into `N8N_WEBHOOK_URL`.
7. If you set `N8N_WEBHOOK_SECRET`, add logic in n8n to require the `X-Webhook-Secret` header.

n8n reference: <https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.webhook/>

## Example `.env`

```dotenv
N8N_WEBHOOK_URL=https://n8n.example.com/webhook/grq-events
N8N_WEBHOOK_SECRET=super-secret

# Alerts only arrive when the matching alert rule includes n8n.
ALERT_PIPELINE_FAILURE_CHANNELS=telegram,email,n8n
ALERT_CIRCUIT_BREAKER_CHANNELS=telegram,n8n
ALERT_LLM_PROVIDER_DOWN_CHANNELS=telegram,n8n
ALERT_HIGH_LATENCY_CHANNELS=email,n8n
ALERT_KILL_SWITCH_CHANNELS=telegram,n8n
ALERT_DB_CONNECTION_CHANNELS=email,pagerduty,n8n
```

## Webhook envelope

All n8n events use the same top-level JSON shape:

```json
{
  "event_type": "signal | decision | alert",
  "severity": "info | warning | critical",
  "timestamp": "RFC3339 timestamp",
  "strategy_id": "optional UUID",
  "pipeline_run_id": "optional UUID",
  "data": {},
  "callback_url": "optional future-use URL"
}
```

Current transport details:

- method: `POST`
- content type: `application/json`
- optional header: `X-Webhook-Secret: <value from N8N_WEBHOOK_SECRET>`

Important: `callback_url` exists in the payload type, but the current runtime does not populate it for the built-in alert/signal/decision emitters.

## Alert payload example

This is the alert payload emitted by the n8n channel today:

```json
{
  "event_type": "alert",
  "severity": "critical",
  "timestamp": "2026-04-02T15:03:00Z",
  "data": {
    "key": "db_connection_loss",
    "title": "Database connection lost",
    "body": "The application could not reach the configured database.",
    "occurred_at": "2026-04-02T15:03:00Z",
    "metadata": {
      "error": "dial tcp: connection refused"
    },
    "text": "[CRITICAL] Database connection lost\nTime: 2026-04-02T15:03:00Z\nThe application could not reach the configured database.\nerror: dial tcp: connection refused"
  }
}
```

## Signal payload example

This is the structured payload used by the smoke/manual-run signal path:

```json
{
  "event_type": "signal",
  "severity": "info",
  "timestamp": "2026-04-02T15:02:00Z",
  "strategy_id": "11111111-2222-3333-4444-555555555555",
  "pipeline_run_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "data": {
    "strategy_name": "Momentum",
    "ticker": "AAPL",
    "signal": "buy",
    "confidence": 0.92,
    "reasoning": "Breakout confirmed.",
    "occurred_at": "2026-04-02T15:02:00Z"
  }
}
```

## Decision payload example

This is the structured payload used by the smoke/manual-run decision path:

```json
{
  "event_type": "decision",
  "severity": "info",
  "timestamp": "2026-04-02T15:01:00Z",
  "strategy_id": "11111111-2222-3333-4444-555555555555",
  "pipeline_run_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "data": {
    "agent_role": "trader",
    "phase": "trading",
    "summary": "{\"action\":\"buy\"}",
    "llm_provider": "openai",
    "llm_model": "gpt-4.1",
    "latency_ms": 842,
    "occurred_at": "2026-04-02T15:01:00Z"
  }
}
```

## Suggested n8n workflow

A practical starting flow is:

1. **Webhook** node receives the JSON event.
2. **IF** or **Switch** node routes on `{{$json.event_type}}`.
3. Branch by type:
   - `signal`: send a short trading update to Discord/Slack.
   - `decision`: archive the decision payload, then post a summarized reviewer message.
   - `alert`: forward `{{$json.data.text}}` to email, Slack, Discord, PagerDuty bridge, or ticketing.
4. Optionally store raw payloads in a database or spreadsheet for later analysis.

Example switch values:

- `signal`
- `decision`
- `alert`

## n8n expression ideas

- event type: `{{$json.event_type}}`
- severity: `{{$json.severity}}`
- alert text: `{{$json.data.text}}`
- signal summary: `{{$json.data.ticker}} {{$json.data.signal}} (confidence {{$json.data.confidence}})`
- decision summary: `{{$json.data.agent_role}} {{$json.data.phase}}: {{$json.data.summary}}`

## Current limitations

- Signal and decision payloads are only emitted by the smoke/manual-run path today.
- In non-smoke environments, the manual run endpoint is not configured, so you should expect alert payloads only unless runtime wiring changes.
- The runtime sends to a single configured n8n endpoint. If you need fan-out, let n8n branch to downstream tools.

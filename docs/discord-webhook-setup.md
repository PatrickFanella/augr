# Discord webhook setup

This guide documents what the runtime emits today.
Prefer the `NOTIFY_DISCORD_*` variable names shown below.
The config loader still accepts legacy aliases for compatibility.

## Current webhook variables

| Purpose | Preferred env var | Legacy aliases still accepted | What it receives today |
| --- | --- | --- | --- |
| signal channel | `NOTIFY_DISCORD_SIGNAL_WEBHOOK_URL` | `DISCORD_WEBHOOK_SIGNALS`, `DISCORD_SIGNAL_WEBHOOK_URL` | Structured trading-signal embeds |
| decision channel | `NOTIFY_DISCORD_DECISION_WEBHOOK_URL` | `DISCORD_WEBHOOK_DECISIONS`, `DISCORD_DECISION_WEBHOOK_URL` | Structured agent-decision embeds |
| alert channel | `NOTIFY_DISCORD_ALERT_WEBHOOK_URL` | `DISCORD_WEBHOOK_ALERTS`, `DISCORD_ALERT_WEBHOOK_URL` | Alert embeds from the alert manager |

## What is actually emitted

| Webhook | Runtime source | Fires when | Caveats |
| --- | --- | --- | --- |
| signal | smoke strategy runner -> `RecordSignal` | a manual strategy run completes and produces a final signal | current code only wires manual runs in `APP_ENV=smoke`; outside smoke, `/run` returns `501 manual strategy runs are not configured` |
| decision | smoke strategy runner -> `RecordDecision` | after the same smoke/manual run, once per stored agent decision | same `APP_ENV=smoke` caveat; one Discord post per decision row |
| alert | alert manager -> `Notify` | an alert rule routes to channel `discord` | alert delivery is controlled by `ALERT_*_CHANNELS`; setting a Discord alert webhook alone does not route alerts there |

## Step-by-step: create a Discord webhook URL

1. Open the target Discord server.
2. Open the channel that should receive the messages.
3. Open **Edit Channel**.
4. Open **Integrations**.
5. Open **Webhooks**.
6. Create a new webhook for that channel.
7. Give it a descriptive name such as `grq-signals`, `grq-decisions`, or `grq-alerts`.
8. Copy the webhook URL.
9. Repeat for each channel you want to separate.

Discord reference: <https://docs.discord.com/developers/resources/webhook>

## Example `.env`

```dotenv
# Preferred names.
NOTIFY_DISCORD_SIGNAL_WEBHOOK_URL=https://discord.com/api/webhooks/...
NOTIFY_DISCORD_DECISION_WEBHOOK_URL=https://discord.com/api/webhooks/...
NOTIFY_DISCORD_ALERT_WEBHOOK_URL=https://discord.com/api/webhooks/...

# Route alert classes to the discord notifier when you want alerts posted.
ALERT_PIPELINE_FAILURE_CHANNELS=telegram,email,discord
ALERT_CIRCUIT_BREAKER_CHANNELS=telegram,discord
ALERT_LLM_PROVIDER_DOWN_CHANNELS=telegram,discord
ALERT_HIGH_LATENCY_CHANNELS=email,discord
ALERT_KILL_SWITCH_CHANNELS=telegram,discord
ALERT_DB_CONNECTION_CHANNELS=email,pagerduty,discord
```

## Signal embed shape

Signal embeds come from `FormatSignalEmbed`.

- title: `Signal: BUY`, `Signal: SELL`, or `Signal: HOLD`
- color: green for buy, red for sell, gray for hold
- fields: `Strategy`, `Ticker`, `Confidence`, `Reasoning`
- footer: `Run <first-8-chars-of-run-id>`
- timestamp: run completion time

Example payload sent to Discord:

```json
{
  "embeds": [
    {
      "title": "Signal: BUY",
      "color": 3066993,
      "fields": [
        { "name": "Strategy", "value": "Momentum", "inline": true },
        { "name": "Ticker", "value": "AAPL", "inline": true },
        { "name": "Confidence", "value": "92.0%", "inline": true },
        { "name": "Reasoning", "value": "Breakout confirmed.", "inline": false }
      ],
      "footer": { "text": "Run aabbccdd" },
      "timestamp": "2026-04-02T15:02:00Z"
    }
  ]
}
```

## Decision embed shape

Decision embeds come from `FormatDecisionEmbed`.

- title: `Decision: <agent role>`
- color: blue
- fields: `Phase`, `Model`, `Latency`, `Output`
- footer: `Run <first-8-chars-of-run-id>`
- timestamp: decision creation time

Important: the event contains both `LLMProvider` and `LLMModel`, but the current Discord embed only renders the model, not the provider.

Example payload sent to Discord:

```json
{
  "embeds": [
    {
      "title": "Decision: trader",
      "color": 3447003,
      "fields": [
        { "name": "Phase", "value": "trading", "inline": true },
        { "name": "Model", "value": "gpt-4.1", "inline": true },
        { "name": "Latency", "value": "842ms", "inline": true },
        { "name": "Output", "value": "{\"action\":\"buy\"}", "inline": false }
      ],
      "footer": { "text": "Run aabbccdd" },
      "timestamp": "2026-04-02T15:01:00Z"
    }
  ]
}
```

## Alert embed shape

Alert embeds come from the alert notifier path, not from `FormatAlertEmbed`.
The live runtime uses the alert title/body directly and converts alert metadata into embed fields.

- title: alert title such as `Kill switch toggled`
- description: alert body
- color: info blue, warning amber, critical red
- fields: sorted alert metadata keys, if any
- timestamp: alert occurrence time

Example payload sent to Discord:

```json
{
  "embeds": [
    {
      "title": "Kill switch toggled",
      "description": "Kill switch was enabled. activated from CLI",
      "color": 15158332,
      "fields": [
        { "name": "source", "value": "cli", "inline": true }
      ],
      "timestamp": "2026-04-02T15:03:00Z"
    }
  ]
}
```

## Smoke/manual-run caveat

If you want signal and decision posts, the current runtime must have a configured manual runner.
Today that only happens when the server starts with `APP_ENV=smoke`.
In non-smoke environments, the manual run endpoint is not wired and signal/decision Discord posts will not fire.

## Operational notes

- Empty Discord webhook variables are treated as disabled; the notifier quietly no-ops for that channel.
- Discord requests retry on HTTP `429` using the `Retry-After` header, up to 5 attempts.
- Alert routing is separate from signal/decision routing. Alert delivery depends on `ALERT_*_CHANNELS`; signal/decision delivery depends on the smoke/manual-run path.

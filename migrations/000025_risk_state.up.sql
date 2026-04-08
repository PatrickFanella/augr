-- Single-row table for persisted runtime risk state.
-- Only API-toggle kill-switch activations are stored here; file and
-- environment-variable mechanisms are inherently durable outside the DB.
CREATE TABLE IF NOT EXISTS risk_state (
    id                   SMALLINT PRIMARY KEY DEFAULT 1,
    kill_switch          JSONB NOT NULL DEFAULT '{}',
    market_kill_switches JSONB NOT NULL DEFAULT '{}',
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT single_row CHECK (id = 1)
);

-- Seed the row so there is always exactly one risk state record.
INSERT INTO risk_state (id) VALUES (1) ON CONFLICT DO NOTHING;

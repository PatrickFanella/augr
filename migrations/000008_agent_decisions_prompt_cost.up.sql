-- 000008_agent_decisions_prompt_cost.up.sql
-- Add prompt_text and cost_usd columns to agent_decisions for observability.

ALTER TABLE agent_decisions
    ADD COLUMN prompt_text TEXT,
    ADD COLUMN cost_usd    NUMERIC(12,6) DEFAULT 0;

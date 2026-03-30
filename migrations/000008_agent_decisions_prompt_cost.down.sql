-- 000008_agent_decisions_prompt_cost.down.sql

ALTER TABLE agent_decisions
    DROP COLUMN IF EXISTS prompt_text,
    DROP COLUMN IF EXISTS cost_usd;

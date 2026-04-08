-- Add active_thesis JSONB column to strategies.
-- Stores the latest LLM-generated thesis for signal-triggered execution.
-- One thesis per strategy; new pipeline runs supersede the previous value.
ALTER TABLE strategies ADD COLUMN IF NOT EXISTS active_thesis JSONB;

-- Store conversation threads for a pipeline run and their messages.
-- pipeline_run_id intentionally has no formal FK because pipeline_runs is
-- partitioned with a composite primary key that includes the partition key.
CREATE TABLE conversations (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_run_id UUID        NOT NULL,
    agent_role      TEXT        NOT NULL,
    title           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE conversation_messages (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    role            TEXT        NOT NULL CHECK (role IN ('user', 'assistant')),
    content         TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION prevent_conversation_message_created_at_update() RETURNS trigger AS $$
BEGIN
    IF NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'conversation_messages.created_at is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_conversation_messages_created_at_immutable
    BEFORE UPDATE OF created_at ON conversation_messages
    FOR EACH ROW EXECUTE FUNCTION prevent_conversation_message_created_at_update();

CREATE INDEX idx_conversations_pipeline_run_id ON conversations (pipeline_run_id);
CREATE INDEX idx_conversations_created_at ON conversations (created_at);
CREATE INDEX idx_conversations_pipeline_run_id_created_at_id ON conversations (pipeline_run_id, created_at, id);
CREATE INDEX idx_conversation_messages_conversation_id ON conversation_messages (conversation_id);
CREATE INDEX idx_conversation_messages_created_at ON conversation_messages (created_at);
CREATE INDEX idx_conversation_messages_conversation_id_created_at_id ON conversation_messages (conversation_id, created_at, id);

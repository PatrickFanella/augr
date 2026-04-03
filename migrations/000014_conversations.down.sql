DO $$
BEGIN
    IF to_regclass(current_schema() || '.conversation_messages') IS NOT NULL THEN
        EXECUTE 'DROP TRIGGER IF EXISTS trg_conversation_messages_created_at_immutable ON conversation_messages;';
    END IF;
END
$$;

DROP FUNCTION IF EXISTS prevent_conversation_message_created_at_update();
DROP TABLE IF EXISTS conversation_messages CASCADE;
DROP TABLE IF EXISTS conversations CASCADE;

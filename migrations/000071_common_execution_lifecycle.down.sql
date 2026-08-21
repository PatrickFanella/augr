-- Quiesce normalization and lifecycle writers before inspecting rollback
-- safety. The lock makes the empty-only decision race-free.
LOCK TABLE economic_event_normalizations, execution_lifecycle_events, execution_fills, execution_order_bindings, execution_orders, execution_intents
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM execution_lifecycle_events) OR
       EXISTS (SELECT 1 FROM execution_fills) OR
       EXISTS (SELECT 1 FROM execution_order_bindings) OR
       EXISTS (SELECT 1 FROM execution_orders) OR
       EXISTS (SELECT 1 FROM execution_intents) OR
       EXISTS (
           SELECT 1 FROM economic_event_normalizations
           WHERE reference_type = 'execution_fill'
       ) THEN
        RAISE EXCEPTION 'cannot roll back migration 71 while common execution lifecycle evidence exists';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_economic_normalizations_execution_fill_complete ON economic_event_normalizations;
DROP TRIGGER IF EXISTS trg_execution_lifecycle_events_complete ON execution_lifecycle_events;
DROP TRIGGER IF EXISTS trg_execution_fills_complete ON execution_fills;
DROP TRIGGER IF EXISTS trg_execution_order_bindings_complete ON execution_order_bindings;
DROP TRIGGER IF EXISTS trg_execution_orders_complete ON execution_orders;
DROP TRIGGER IF EXISTS trg_execution_intents_complete ON execution_intents;
DROP FUNCTION IF EXISTS validate_execution_normalization_complete();
DROP FUNCTION IF EXISTS validate_execution_binding_complete();
DROP FUNCTION IF EXISTS validate_execution_child_complete();
DROP FUNCTION IF EXISTS validate_execution_intent_complete();
DROP FUNCTION IF EXISTS assert_execution_fill_normalization(UUID);
DROP FUNCTION IF EXISTS assert_execution_lifecycle_graph(UUID);
DROP TRIGGER IF EXISTS trg_execution_lifecycle_events_validate ON execution_lifecycle_events;
DROP FUNCTION IF EXISTS validate_execution_lifecycle_event();
DROP FUNCTION IF EXISTS execution_lifecycle_transition_is_valid(TEXT, TEXT, TEXT, TEXT);
DROP TRIGGER IF EXISTS trg_execution_fills_validate ON execution_fills;
DROP FUNCTION IF EXISTS validate_execution_fill();
DROP TRIGGER IF EXISTS trg_execution_order_bindings_validate ON execution_order_bindings;
DROP FUNCTION IF EXISTS validate_execution_order_binding();
DROP TRIGGER IF EXISTS trg_execution_orders_validate ON execution_orders;
DROP FUNCTION IF EXISTS validate_execution_order();
DROP TRIGGER IF EXISTS trg_execution_intents_validate ON execution_intents;
DROP FUNCTION IF EXISTS validate_execution_intent();
DROP TRIGGER IF EXISTS trg_execution_lifecycle_events_immutable ON execution_lifecycle_events;
DROP TRIGGER IF EXISTS trg_execution_fills_immutable ON execution_fills;
DROP TRIGGER IF EXISTS trg_execution_order_bindings_immutable ON execution_order_bindings;
DROP TRIGGER IF EXISTS trg_execution_orders_immutable ON execution_orders;
DROP TRIGGER IF EXISTS trg_execution_intents_immutable ON execution_intents;
DROP FUNCTION IF EXISTS reject_execution_lifecycle_mutation();
DROP TABLE execution_lifecycle_events;
DROP TABLE execution_fills;
DROP TABLE execution_order_bindings;
DROP TABLE execution_orders;
DROP TABLE execution_intents;

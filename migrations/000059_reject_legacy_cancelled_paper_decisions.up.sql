DO $$
DECLARE
    audited_target_count integer;
    candidate_count integer;
    distinct_paper_order_count integer;
    filled_target_count integer;
    trade_link_count integer;
    duplicate_order_count integer;
BEGIN
    WITH audited_targets AS (
        SELECT DISTINCT
            td.id AS decision_id,
            td.paper_order_id,
            td.risk_reasons,
            o.id AS order_id
        FROM trade_decisions td
        JOIN orders o ON o.id = td.paper_order_id
        WHERE td.market_type = 'kalshi'
          AND o.market_type = 'kalshi'
          AND o.broker = 'paper'
          AND o.order_type = 'limit'
          AND td.status = 'paper_ordered'
          AND o.status = 'cancelled'
          AND EXISTS (
              SELECT 1
              FROM audit_log al
              WHERE al.event_type = 'legacy_paper_order_cancelled'
                AND al.entity_type = 'order'
                AND al.entity_id = o.id
                AND al.actor = 'operator:p0_backfill'
                AND al.details ->> 'reason' = 'missing_historical_reference_price'
          )
    ), candidate_decisions AS (
        SELECT
            at.*,
            NOT ('legacy_paper_order_cancelled' = ANY(at.risk_reasons)) AS reason_added
        FROM audited_targets at
        WHERE at.order_id IN (
            SELECT o.id
            FROM orders o
            WHERE o.id = at.order_id
              AND o.filled_quantity = 0
              AND o.filled_avg_price IS NULL
              AND o.filled_at IS NULL
              AND NOT EXISTS (SELECT 1 FROM trades t WHERE t.order_id = o.id)
        )
    )
    SELECT
        (SELECT COUNT(*)::integer FROM audited_targets),
        (SELECT COUNT(*)::integer FROM candidate_decisions),
        (SELECT COUNT(DISTINCT paper_order_id)::integer FROM candidate_decisions),
        (SELECT COUNT(*)::integer FROM audited_targets WHERE EXISTS (SELECT 1 FROM orders o WHERE o.id = audited_targets.order_id AND (o.filled_quantity <> 0 OR o.filled_avg_price IS NOT NULL OR o.filled_at IS NOT NULL))),
        (SELECT COUNT(*)::integer FROM audited_targets WHERE EXISTS (SELECT 1 FROM trades t WHERE t.order_id = audited_targets.order_id)),
        (SELECT COUNT(*)::integer FROM (SELECT order_id FROM audited_targets GROUP BY order_id HAVING COUNT(*) > 1) duplicate_orders)
    INTO audited_target_count, candidate_count, distinct_paper_order_count, filled_target_count, trade_link_count, duplicate_order_count;

    IF audited_target_count = 0 THEN
        RETURN;
    END IF;

    IF filled_target_count > 0 OR trade_link_count > 0 THEN
        RAISE EXCEPTION 'migration 000059 aborted: audited target orders must be fill-free and trade-free';
    END IF;

    IF duplicate_order_count > 0 THEN
        RAISE EXCEPTION 'migration 000059 aborted: duplicate candidate decisions per order are not allowed';
    END IF;

    IF audited_target_count <> candidate_count OR candidate_count <> distinct_paper_order_count THEN
        RAISE EXCEPTION 'migration 000059 aborted: audited target count, candidate count, and distinct paper_order_id count must match';
    END IF;

    WITH audited_targets AS (
        SELECT DISTINCT
            td.id AS decision_id,
            td.paper_order_id,
            td.risk_reasons,
            o.id AS order_id
        FROM trade_decisions td
        JOIN orders o ON o.id = td.paper_order_id
        WHERE td.market_type = 'kalshi'
          AND o.market_type = 'kalshi'
          AND o.broker = 'paper'
          AND o.order_type = 'limit'
          AND td.status = 'paper_ordered'
          AND o.status = 'cancelled'
          AND EXISTS (
              SELECT 1
              FROM audit_log al
              WHERE al.event_type = 'legacy_paper_order_cancelled'
                AND al.entity_type = 'order'
                AND al.entity_id = o.id
                AND al.actor = 'operator:p0_backfill'
                AND al.details ->> 'reason' = 'missing_historical_reference_price'
          )
    ), candidate_decisions AS (
        SELECT
            at.*,
            NOT ('legacy_paper_order_cancelled' = ANY(at.risk_reasons)) AS reason_added
        FROM audited_targets at
        WHERE at.order_id IN (
            SELECT o.id
            FROM orders o
            WHERE o.id = at.order_id
              AND o.filled_quantity = 0
              AND o.filled_avg_price IS NULL
              AND o.filled_at IS NULL
              AND NOT EXISTS (SELECT 1 FROM trades t WHERE t.order_id = o.id)
        )
    ), updated_decisions AS (
        UPDATE trade_decisions td
        SET status = 'rejected',
            risk_reasons = CASE
                WHEN 'legacy_paper_order_cancelled' = ANY(td.risk_reasons) THEN td.risk_reasons
                ELSE array_append(td.risk_reasons, 'legacy_paper_order_cancelled')
            END,
            updated_at = NOW()
        FROM candidate_decisions cd
        WHERE td.id = cd.decision_id
        RETURNING td.id, td.paper_order_id, cd.reason_added
    )
    INSERT INTO audit_log (event_type, entity_type, entity_id, actor, details)
    SELECT
        'legacy_paper_decision_rejected',
        'trade_decision',
        ud.id,
        'migration:000059',
        jsonb_build_object(
            'paper_order_id', ud.paper_order_id,
            'reason', 'legacy_paper_order_cancelled',
            'reason_added', ud.reason_added
        )
    FROM updated_decisions ud;
END $$;

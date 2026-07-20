WITH reverted_decisions AS (
    SELECT
        td.id,
        td.risk_reasons,
        (al.details ->> 'reason_added')::boolean AS reason_added
    FROM trade_decisions td
    JOIN audit_log al ON al.entity_id = td.id
    WHERE td.status = 'rejected'
      AND al.event_type = 'legacy_paper_decision_rejected'
      AND al.entity_type = 'trade_decision'
      AND al.actor = 'migration:000059'
      AND al.details ->> 'reason' = 'legacy_paper_order_cancelled'
    FOR UPDATE OF td
), restored AS (
    UPDATE trade_decisions td
    SET status = 'paper_ordered',
        risk_reasons = CASE
            WHEN rd.reason_added THEN array_remove(td.risk_reasons, 'legacy_paper_order_cancelled')
            ELSE td.risk_reasons
        END,
        updated_at = NOW()
    FROM reverted_decisions rd
    WHERE td.id = rd.id
    RETURNING td.id
)
DELETE FROM audit_log al
USING restored r
WHERE al.entity_id = r.id
  AND al.event_type = 'legacy_paper_decision_rejected'
  AND al.entity_type = 'trade_decision'
  AND al.actor = 'migration:000059';

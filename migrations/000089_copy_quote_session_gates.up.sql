LOCK TABLE copy_subscriptions, copy_trade_intents, quote_snapshots IN ACCESS EXCLUSIVE MODE;

ALTER TABLE copy_subscriptions
    ADD COLUMN max_quote_age_seconds INT NOT NULL DEFAULT 60
        CHECK (max_quote_age_seconds BETWEEN 1 AND 3600),
    ADD COLUMN allowed_sessions TEXT[] NOT NULL DEFAULT ARRAY['regular']::TEXT[]
        CHECK (cardinality(allowed_sessions) > 0 AND allowed_sessions <@ ARRAY['regular','pre_market','after_hours']::TEXT[]);

ALTER TABLE copy_trade_intents
    ADD COLUMN quote_gate_version INT NOT NULL DEFAULT 0 CHECK (quote_gate_version IN (0,1)),
    ADD COLUMN decision_quote_snapshot_id UUID REFERENCES quote_snapshots(id),
    ADD COLUMN decision_bid NUMERIC(30,12),
    ADD COLUMN decision_ask NUMERIC(30,12),
    ADD COLUMN decision_spread_bps NUMERIC(30,12),
    ADD COLUMN decision_available_at TIMESTAMPTZ,
    ADD COLUMN decision_at TIMESTAMPTZ,
    ADD COLUMN decision_market_status TEXT,
    ADD COLUMN decision_session_status TEXT,
    ADD CONSTRAINT copy_intent_quote_gate_shape CHECK (
        (quote_gate_version=0 AND decision_quote_snapshot_id IS NULL AND decision_bid IS NULL AND decision_ask IS NULL AND decision_spread_bps IS NULL AND decision_available_at IS NULL AND decision_at IS NULL AND decision_market_status IS NULL AND decision_session_status IS NULL)
        OR
        (quote_gate_version=1 AND decision_at IS NOT NULL AND (
            policy_status <> 'approved' OR
            (decision_quote_snapshot_id IS NOT NULL AND decision_bid > 0 AND decision_ask >= decision_bid AND decision_spread_bps >= 0 AND decision_available_at IS NOT NULL AND decision_market_status='open' AND decision_session_status IS NOT NULL AND executable_price IS NOT NULL)
        ))
    );

CREATE OR REPLACE FUNCTION copy_intent_quote_gate_guard() RETURNS TRIGGER AS $$
DECLARE
    quote quote_snapshots%ROWTYPE;
    subscription copy_subscriptions%ROWTYPE;
    expected_spread NUMERIC;
    expected_price NUMERIC;
BEGIN
    IF NEW.quote_gate_version = 0 THEN
        RETURN NEW;
    END IF;
    SELECT * INTO subscription FROM copy_subscriptions WHERE id=NEW.subscription_id;
    IF NEW.decision_quote_snapshot_id IS NOT NULL THEN
        SELECT * INTO quote FROM quote_snapshots WHERE id=NEW.decision_quote_snapshot_id;
        IF NOT FOUND OR quote.bid IS NULL OR quote.ask IS NULL OR
           quote.bid::NUMERIC <> NEW.decision_bid OR quote.ask::NUMERIC <> NEW.decision_ask OR
           quote.available_at IS NULL OR quote.available_at <> NEW.decision_available_at OR
           quote.market_status <> NEW.decision_market_status OR quote.session_status <> NEW.decision_session_status THEN
            RAISE EXCEPTION 'copy intent decision quote does not reconstruct';
        END IF;
    END IF;
    IF NEW.policy_status = 'approved' THEN
        expected_spread := round((quote.ask-quote.bid)/((quote.ask+quote.bid)/2)*10000,12);
        expected_price := CASE WHEN NEW.side='buy' THEN quote.ask ELSE quote.bid END;
        IF expected_spread <> NEW.decision_spread_bps OR expected_price <> NEW.executable_price OR
           NEW.decision_available_at > NEW.decision_at OR
           NEW.decision_at-NEW.decision_available_at > make_interval(secs => subscription.max_quote_age_seconds) OR
           NEW.decision_market_status <> 'open' OR NOT (NEW.decision_session_status = ANY(subscription.allowed_sessions)) OR
           NEW.decision_spread_bps > subscription.max_spread_bps THEN
            RAISE EXCEPTION 'copy intent quote policy does not reconstruct';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER copy_intent_quote_gate_guard
BEFORE INSERT OR UPDATE ON copy_trade_intents
FOR EACH ROW EXECUTE FUNCTION copy_intent_quote_gate_guard();

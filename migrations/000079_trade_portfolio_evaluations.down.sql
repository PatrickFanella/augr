LOCK TABLE evaluation_metrics,evaluation_trade_fill_ids,evaluation_closed_trades,evaluation_observations,
  trade_portfolio_evaluations,evaluation_policy_artifacts IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
  IF EXISTS(SELECT 1 FROM trade_portfolio_evaluations) OR EXISTS(SELECT 1 FROM evaluation_policy_artifacts) THEN
    RAISE EXCEPTION 'cannot roll back migration 79 while evaluation evidence exists';
  END IF;
END $$;
DROP TABLE evaluation_metrics;
DROP TABLE evaluation_trade_fill_ids;
DROP TABLE evaluation_closed_trades;
DROP TABLE evaluation_observations;
DROP TABLE trade_portfolio_evaluations;
DROP TABLE evaluation_policy_artifacts;
DROP FUNCTION reject_trade_portfolio_evaluation_mutation();
DROP FUNCTION validate_trade_portfolio_evaluation_graph();
DROP FUNCTION trade_portfolio_evaluation_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION evaluation_metric_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION evaluation_trade_identity(INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION evaluation_observation_identity(INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION evaluation_policy_identity(TEXT,TEXT,INTEGER,TEXT,TEXT,TEXT,TEXT,INTEGER);
DROP FUNCTION evaluation_decimal_valid(TEXT);

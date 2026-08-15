DROP TRIGGER IF EXISTS trg_capital_flows_to_ledger ON capital_flows;
DROP FUNCTION IF EXISTS post_capital_flow_to_ledger();

DROP TABLE IF EXISTS projection_checkpoints;
DROP TABLE IF EXISTS mark_observations;
DROP TABLE IF EXISTS ledger_postings;
DROP TABLE IF EXISTS ledger_transactions;

DROP FUNCTION IF EXISTS validate_ledger_posting_balance();
DROP FUNCTION IF EXISTS validate_ledger_transaction_row_balance();
DROP FUNCTION IF EXISTS assert_ledger_transaction_balanced(UUID);
DROP FUNCTION IF EXISTS reject_ledger_mutation();

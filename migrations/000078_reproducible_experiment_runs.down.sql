LOCK TABLE experiment_run_attempt_events,experiment_run_fill_ids,experiment_run_transition_ids,
  experiment_run_step_outcomes,experiment_run_results,experiment_run_attempts,experiment_replay_plan_steps,
  experiment_replay_plans,experiment_programs IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
  IF EXISTS(SELECT 1 FROM experiment_programs) OR EXISTS(SELECT 1 FROM experiment_replay_plans) OR
    EXISTS(SELECT 1 FROM experiment_run_attempts) OR EXISTS(SELECT 1 FROM experiment_run_results) THEN
    RAISE EXCEPTION 'cannot roll back migration 78 while experiment run evidence exists';
  END IF;
END $$;
DROP TABLE experiment_run_attempt_events;
DROP TABLE experiment_run_fill_ids;
DROP TABLE experiment_run_transition_ids;
DROP TABLE experiment_run_step_outcomes;
DROP TABLE experiment_run_results;
DROP TABLE experiment_run_attempts;
DROP TABLE experiment_replay_plan_steps;
DROP TABLE experiment_replay_plans;
DROP TABLE experiment_programs;
DROP FUNCTION reject_experiment_run_mutation();
DROP FUNCTION validate_experiment_attempt_event();
DROP FUNCTION validate_experiment_result_graph();
DROP FUNCTION validate_experiment_plan_graph();
DROP FUNCTION validate_experiment_program();
DROP FUNCTION experiment_result_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION experiment_metrics_identity(INTEGER,INTEGER,INTEGER,INTEGER,INTEGER,INTEGER,INTEGER,TEXT,TEXT);
DROP FUNCTION experiment_step_outcome_identity(INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION experiment_attempt_event_identity(TEXT,INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION experiment_plan_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,BIGINT,TEXT,TEXT);
DROP FUNCTION experiment_plan_step_identity(INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION experiment_plan_intent_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION experiment_program_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION experiment_capital_state_identity(JSONB);

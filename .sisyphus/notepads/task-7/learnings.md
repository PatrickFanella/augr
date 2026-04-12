## task-7-health
- export_test.go only compiles within same package; cross-package test helpers must be real exported methods
- Server.automation field is *automation.JobOrchestrator (concrete); set directly in test struct literal
- Pre-existing LSP errors in orchestrator.go (JobRunSummary fields) and cmd/ — unrelated

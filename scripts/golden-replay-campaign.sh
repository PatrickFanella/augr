#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"

candidate_commit=$(git rev-parse HEAD)
echo "Starting OVR-701 golden replay campaign for commit $candidate_commit."

go test -race -count=2 ./internal/ledger ./internal/execution/lifecycle \
  ./internal/execution/prediction ./internal/risk

go test -race -count=2 ./internal/repository/postgres -run \
  '^(TestExperimentRunnerGolden(PostgresReplayAndRestart|PartialFill|ExplicitNoopAndRejection|CleanDatabaseReproduction|ScoredStressIsolation|FailedCompletionThenRetry|ResultChildStageRollback)|TestCapitalGoldenReplayPersistsReloadsAndReplaysWithoutDuplication|TestPortfolioProjectionRepo(RebuildsAndPersistsExactCheckpoint|ConcurrentIdenticalRebuildsConverge|FailureLeavesEvidenceUntouched|LateBackdatedInputCreatesCorrectedCheckpoint)|TestVenueReconciliation(GoldenAlpacaAndKalshi|GoldenOrphanObservationRemainsNonClean|GoldenIndependentPerturbations|RestartConvergesAfterEveryPersistedStage|GoldenCorrectionBustAndUnstableRemainNonClean)|TestExecutionLifecycleRepoRollsBackWholeFillGraphOnChildFailure)$'

verified_commit=$(git rev-parse HEAD)
if ! [ "$verified_commit" = "$candidate_commit" ]; then
  echo "campaign commit changed: expected $candidate_commit, found $verified_commit" >&2
  exit 1
fi

echo "OVR-701 golden replay campaign passed twice for commit $verified_commit."

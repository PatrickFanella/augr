#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"

echo "Running emergency-brake mechanism, persistence, and reduce-only drills."
go test -count=1 ./internal/risk -run 'Test(CheckPreTrade_EmergencyBrakeAllowsExplicitReduceOnly|EmergencyBrake|KillSwitch_(APIToggle|FileFlagDetection|EnvVarDetection|AnyMechanismBlocksTrading))'
go test -count=1 ./internal/execution -run 'Test(ProcessSignal_KillSwitchActive|ProcessSignal_KillSwitchAdmitsVerifiedReduceOnlyStockExit|ProcessSpreadSignalAtomicallyClosesPersistedLegGroup)'
echo "Emergency-brake drill passed: entry halt, verified reduce-only, restart persistence, and API/file/env mechanisms."

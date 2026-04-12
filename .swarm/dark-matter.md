## Dark Matter: Hidden Couplings

Found 20 file pairs that frequently co-change but have no import relationship:

| File A | File B | NPMI | Co-Changes | Lift |
|--------|--------|------|------------|------|
| internal/agent/rules/reviewer.go | internal/agent/rules/reviewer_test.go | 1.000 | 3 | 166.67 |
| internal/agent/analysts/news_test.go | internal/agent/analysts/social_test.go | 1.000 | 3 | 166.67 |
| internal/agent/event.go | internal/agent/types_test.go | 1.000 | 3 | 166.67 |
| web/src/components/pipeline/decision-inspector.tsx | web/src/components/pipeline/phase-progress.tsx | 1.000 | 4 | 125.00 |
| web/src/components/ui/button.tsx | web/src/components/ui/card.tsx | 1.000 | 3 | 166.67 |
| web/src/components/ui/button.tsx | web/src/index.css | 1.000 | 3 | 166.67 |
| web/src/components/ui/card.tsx | web/src/index.css | 1.000 | 3 | 166.67 |
| web/src/components/ui/input.tsx | web/src/components/ui/textarea.tsx | 1.000 | 3 | 166.67 |
| internal/agent/debate/bear_researcher.go | internal/agent/debate/bull_researcher.go | 1.000 | 3 | 166.67 |
| internal/agent/risk/aggressive.go | internal/agent/risk/conservative.go | 1.000 | 3 | 166.67 |
| web/src/components/pipeline/analyst-cards.tsx | web/src/components/pipeline/debate-view.tsx | 1.000 | 3 | 166.67 |
| web/src/components/pipeline/analyst-cards.tsx | web/src/components/pipeline/final-signal.tsx | 1.000 | 3 | 166.67 |
| web/src/components/pipeline/debate-view.tsx | web/src/components/pipeline/final-signal.tsx | 1.000 | 3 | 166.67 |
| web/src/components/chat/chat-panel.test.tsx | web/src/pages/realtime-page.test.tsx | 1.000 | 3 | 166.67 |
| web/src/components/chat/chat-panel.test.tsx | web/src/pages/risk-page.test.tsx | 1.000 | 3 | 166.67 |
| web/src/pages/realtime-page.test.tsx | web/src/pages/risk-page.test.tsx | 1.000 | 3 | 166.67 |
| migrations/000012_strategies_status.up.sql | migrations/strategies_status_migration_test.go | 1.000 | 5 | 100.00 |
| internal/cli/tui/model.go | internal/cli/tui/websocket.go | 1.000 | 3 | 166.67 |
| internal/cli/tui/model.go | internal/cli/tui/websocket_test.go | 1.000 | 3 | 166.67 |
| internal/cli/tui/websocket.go | internal/cli/tui/websocket_test.go | 1.000 | 3 | 166.67 |

These pairs likely share an architectural concern invisible to static analysis.
Consider adding explicit documentation or extracting the shared concern.
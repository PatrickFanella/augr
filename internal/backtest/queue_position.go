package backtest

import "github.com/PatrickFanella/get-rich-quick/internal/simulation"

type QueueEntry = simulation.QueueEntry
type QueueTracker = simulation.QueueTracker

func NewQueueTracker() *QueueTracker {
	return simulation.NewQueueTracker()
}

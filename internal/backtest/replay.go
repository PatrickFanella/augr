package backtest

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// Errors returned by ReplayIterator.
var (
	// ErrNoData indicates the iterator was created with an empty bar slice.
	ErrNoData = errors.New("backtest: no bars provided")

	// ErrNotStarted indicates Current or SimTime was called before the first
	// call to Next.
	ErrNotStarted = errors.New("backtest: iterator not started; call Next first")

	// ErrFutureAccess indicates an attempt to access data with a timestamp
	// beyond the current simulated time.
	ErrFutureAccess = errors.New("backtest: future data access denied")

	// ErrBarNotFound indicates no bar exists at the requested timestamp.
	ErrBarNotFound = errors.New("backtest: no bar at timestamp")
)

// ReplayIterator steps through a sorted slice of historical OHLCV bars one at
// a time. At any point during the replay, only data with timestamps <= the
// current simulated time is accessible, preventing look-ahead bias.
type ReplayIterator struct {
	bars  []domain.OHLCV // sorted ascending by timestamp
	index int            // current position; -1 means not started
	clock *SimulatedClock
}

// NewReplayIterator creates a ReplayIterator from the given bars.
// Bars are copied and sorted by timestamp in ascending order.
// Returns ErrNoData if bars is empty.
func NewReplayIterator(bars []domain.OHLCV) (*ReplayIterator, error) {
	if len(bars) == 0 {
		return nil, ErrNoData
	}

	sorted := make([]domain.OHLCV, len(bars))
	copy(sorted, bars)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	return &ReplayIterator{
		bars:  sorted,
		index: -1,
		clock: newSimulatedClock(),
	}, nil
}

// Next advances the iterator to the next bar. It returns true if a bar is
// available and false when all bars have been consumed.
func (r *ReplayIterator) Next() bool {
	if r.index+1 >= len(r.bars) {
		return false
	}
	r.index++
	r.clock.set(r.bars[r.index].Timestamp)
	return true
}

// Current returns the bar at the current iterator position.
// Returns ErrNotStarted if Next has not been called yet.
func (r *ReplayIterator) Current() (domain.OHLCV, error) {
	if r.index < 0 {
		return domain.OHLCV{}, ErrNotStarted
	}
	return r.bars[r.index], nil
}

// SimTime returns the timestamp of the current bar, representing the simulated
// clock. Returns ErrNotStarted if Next has not been called yet.
func (r *ReplayIterator) SimTime() (time.Time, error) {
	if r.index < 0 {
		return time.Time{}, ErrNotStarted
	}
	return r.bars[r.index].Timestamp, nil
}

// Bars returns all bars with timestamps <= the current simulated time.
// Returns ErrNotStarted if Next has not been called yet.
func (r *ReplayIterator) Bars() ([]domain.OHLCV, error) {
	if r.index < 0 {
		return nil, ErrNotStarted
	}
	// index+1 because index is zero-based and all bars 0..index are visible.
	result := make([]domain.OHLCV, r.index+1)
	copy(result, r.bars[:r.index+1])
	return result, nil
}

// BarAt returns the bar whose timestamp matches t exactly.
// Returns ErrFutureAccess if t is after the current simulated time, or
// ErrNotStarted if Next has not been called yet.
// Returns ErrBarNotFound (wrapped) if no bar exists at the requested timestamp.
func (r *ReplayIterator) BarAt(t time.Time) (domain.OHLCV, error) {
	if r.index < 0 {
		return domain.OHLCV{}, ErrNotStarted
	}
	simTime := r.bars[r.index].Timestamp
	if t.After(simTime) {
		return domain.OHLCV{}, fmt.Errorf("%w: requested %s, sim time %s", ErrFutureAccess, t, simTime)
	}
	visible := r.bars[:r.index+1]
	i := sort.Search(len(visible), func(i int) bool {
		return !visible[i].Timestamp.Before(t)
	})
	if i < len(visible) && visible[i].Timestamp.Equal(t) {
		return visible[i], nil
	}
	return domain.OHLCV{}, fmt.Errorf("%w: %s", ErrBarNotFound, t)
}

// BarsInRange returns bars within [from, to] that have timestamps <= the
// current simulated time. Returns ErrFutureAccess if to is after the current
// simulated time, or ErrNotStarted if Next has not been called yet.
func (r *ReplayIterator) BarsInRange(from, to time.Time) ([]domain.OHLCV, error) {
	if r.index < 0 {
		return nil, ErrNotStarted
	}
	simTime := r.bars[r.index].Timestamp
	if to.After(simTime) {
		return nil, fmt.Errorf("%w: requested to %s, sim time %s", ErrFutureAccess, to, simTime)
	}
	visible := r.bars[:r.index+1]

	start := sort.Search(len(visible), func(i int) bool {
		return !visible[i].Timestamp.Before(from)
	})
	end := sort.Search(len(visible), func(i int) bool {
		return visible[i].Timestamp.After(to)
	})

	if start >= end {
		return []domain.OHLCV{}, nil
	}

	result := make([]domain.OHLCV, end-start)
	copy(result, visible[start:end])
	return result, nil
}

// Remaining returns the number of unconsumed bars.
func (r *ReplayIterator) Remaining() int {
	return len(r.bars) - r.index - 1
}

// Len returns the total number of bars in the iterator.
func (r *ReplayIterator) Len() int {
	return len(r.bars)
}

// Done returns true when all bars have been consumed.
func (r *ReplayIterator) Done() bool {
	return r.index >= len(r.bars)-1
}

// Clock returns the simulated wall clock that advances with each consumed bar.
func (r *ReplayIterator) Clock() Clock {
	if r == nil {
		return nil
	}

	return r.clock
}

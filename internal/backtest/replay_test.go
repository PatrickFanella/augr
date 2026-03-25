package backtest

import (
	"errors"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func makeBar(ts time.Time, close float64) domain.OHLCV {
	return domain.OHLCV{
		Timestamp: ts,
		Open:      close - 1,
		High:      close + 1,
		Low:       close - 2,
		Close:     close,
		Volume:    1000,
	}
}

func TestNewReplayIteratorRejectsEmptyBars(t *testing.T) {
	_, err := NewReplayIterator(nil)
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("NewReplayIterator(nil) error = %v, want %v", err, ErrNoData)
	}

	_, err = NewReplayIterator([]domain.OHLCV{})
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("NewReplayIterator([]) error = %v, want %v", err, ErrNoData)
	}
}

func TestNewReplayIteratorSortsBars(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)

	// Provide bars out of order.
	iter, err := NewReplayIterator([]domain.OHLCV{
		makeBar(t3, 103),
		makeBar(t1, 101),
		makeBar(t2, 102),
	})
	if err != nil {
		t.Fatalf("NewReplayIterator() error = %v", err)
	}

	// Walk through and verify ascending order.
	expected := []time.Time{t1, t2, t3}
	for i, exp := range expected {
		if !iter.Next() {
			t.Fatalf("Next() = false at step %d", i)
		}
		bar, err := iter.Current()
		if err != nil {
			t.Fatalf("Current() error = %v at step %d", err, i)
		}
		if !bar.Timestamp.Equal(exp) {
			t.Fatalf("bar[%d].Timestamp = %s, want %s", i, bar.Timestamp, exp)
		}
	}
}

func TestNewReplayIteratorDoesNotMutateInput(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)

	input := []domain.OHLCV{makeBar(t2, 102), makeBar(t1, 101)}
	_, err := NewReplayIterator(input)
	if err != nil {
		t.Fatalf("NewReplayIterator() error = %v", err)
	}

	// Original slice should be unmodified.
	if !input[0].Timestamp.Equal(t2) {
		t.Fatal("NewReplayIterator mutated input slice")
	}
}

func TestCurrentBeforeNextReturnsErrNotStarted(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	iter, _ := NewReplayIterator([]domain.OHLCV{makeBar(t1, 100)})

	_, err := iter.Current()
	if !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Current() before Next() error = %v, want %v", err, ErrNotStarted)
	}
}

func TestSimTimeBeforeNextReturnsErrNotStarted(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	iter, _ := NewReplayIterator([]domain.OHLCV{makeBar(t1, 100)})

	_, err := iter.SimTime()
	if !errors.Is(err, ErrNotStarted) {
		t.Fatalf("SimTime() before Next() error = %v, want %v", err, ErrNotStarted)
	}
}

func TestBarsBeforeNextReturnsErrNotStarted(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	iter, _ := NewReplayIterator([]domain.OHLCV{makeBar(t1, 100)})

	_, err := iter.Bars()
	if !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Bars() before Next() error = %v, want %v", err, ErrNotStarted)
	}
}

func TestNextStepsSequentially(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)

	bars := []domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
		makeBar(t3, 103),
	}

	iter, _ := NewReplayIterator(bars)

	for i, expected := range bars {
		if !iter.Next() {
			t.Fatalf("Next() = false at step %d", i)
		}
		bar, err := iter.Current()
		if err != nil {
			t.Fatalf("Current() error = %v at step %d", err, i)
		}
		if bar.Close != expected.Close {
			t.Fatalf("bar[%d].Close = %f, want %f", i, bar.Close, expected.Close)
		}
	}

	// After all bars consumed, Next returns false.
	if iter.Next() {
		t.Fatal("Next() = true after all bars consumed")
	}
}

func TestSimTimeTracksCurrentBar(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)

	iter, _ := NewReplayIterator([]domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
	})

	iter.Next()
	simTime, err := iter.SimTime()
	if err != nil {
		t.Fatalf("SimTime() error = %v", err)
	}
	if !simTime.Equal(t1) {
		t.Fatalf("SimTime() = %s, want %s", simTime, t1)
	}

	iter.Next()
	simTime, err = iter.SimTime()
	if err != nil {
		t.Fatalf("SimTime() error = %v", err)
	}
	if !simTime.Equal(t2) {
		t.Fatalf("SimTime() = %s, want %s", simTime, t2)
	}
}

func TestBarsOnlyReturnsUpToSimTime(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)

	iter, _ := NewReplayIterator([]domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
		makeBar(t3, 103),
	})

	// After first Next, only first bar visible.
	iter.Next()
	visible, err := iter.Bars()
	if err != nil {
		t.Fatalf("Bars() error = %v", err)
	}
	if len(visible) != 1 {
		t.Fatalf("len(Bars()) = %d, want 1", len(visible))
	}

	// After second Next, first two bars visible.
	iter.Next()
	visible, err = iter.Bars()
	if err != nil {
		t.Fatalf("Bars() error = %v", err)
	}
	if len(visible) != 2 {
		t.Fatalf("len(Bars()) = %d, want 2", len(visible))
	}

	// After third Next, all bars visible.
	iter.Next()
	visible, err = iter.Bars()
	if err != nil {
		t.Fatalf("Bars() error = %v", err)
	}
	if len(visible) != 3 {
		t.Fatalf("len(Bars()) = %d, want 3", len(visible))
	}
}

func TestBarsReturnsDefensiveCopy(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	iter, _ := NewReplayIterator([]domain.OHLCV{makeBar(t1, 100)})
	iter.Next()

	bars, _ := iter.Bars()
	bars[0].Close = 999

	current, _ := iter.Current()
	if current.Close == 999 {
		t.Fatal("Bars() returned a reference to internal data; expected a copy")
	}
}

func TestBarAtRejectsAccessBeyondSimTime(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)

	iter, _ := NewReplayIterator([]domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
	})
	iter.Next() // simTime = t1

	_, err := iter.BarAt(t2)
	if !errors.Is(err, ErrFutureAccess) {
		t.Fatalf("BarAt(future) error = %v, want %v", err, ErrFutureAccess)
	}
}

func TestBarAtReturnsCurrentBar(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	iter, _ := NewReplayIterator([]domain.OHLCV{makeBar(t1, 101)})
	iter.Next()

	bar, err := iter.BarAt(t1)
	if err != nil {
		t.Fatalf("BarAt() error = %v", err)
	}
	if bar.Close != 101 {
		t.Fatalf("BarAt().Close = %f, want 101", bar.Close)
	}
}

func TestBarAtReturnsPastBar(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)

	iter, _ := NewReplayIterator([]domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
	})
	iter.Next()
	iter.Next() // simTime = t2

	bar, err := iter.BarAt(t1)
	if err != nil {
		t.Fatalf("BarAt(past) error = %v", err)
	}
	if bar.Close != 101 {
		t.Fatalf("BarAt(past).Close = %f, want 101", bar.Close)
	}
}

func TestBarAtReturnsErrorForMissingTimestamp(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	tMissing := t1.Add(12 * time.Hour)

	iter, _ := NewReplayIterator([]domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
	})
	iter.Next()
	iter.Next()

	_, err := iter.BarAt(tMissing)
	if err == nil {
		t.Fatal("BarAt(missing) expected error, got nil")
	}
	if errors.Is(err, ErrFutureAccess) {
		t.Fatal("BarAt(missing) should not be ErrFutureAccess")
	}
	if !errors.Is(err, ErrBarNotFound) {
		t.Fatalf("BarAt(missing) error = %v, want %v", err, ErrBarNotFound)
	}
}

func TestBarAtBeforeNextReturnsErrNotStarted(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	iter, _ := NewReplayIterator([]domain.OHLCV{makeBar(t1, 100)})

	_, err := iter.BarAt(t1)
	if !errors.Is(err, ErrNotStarted) {
		t.Fatalf("BarAt() before Next() error = %v, want %v", err, ErrNotStarted)
	}
}

func TestBarsInRangeRejectsAccessBeyondSimTime(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)

	iter, _ := NewReplayIterator([]domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
		makeBar(t3, 103),
	})
	iter.Next() // simTime = t1

	_, err := iter.BarsInRange(t1, t2)
	if !errors.Is(err, ErrFutureAccess) {
		t.Fatalf("BarsInRange() to future error = %v, want %v", err, ErrFutureAccess)
	}
}

func TestBarsInRangeReturnsMatchingBars(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)
	t4 := t3.Add(24 * time.Hour)

	iter, _ := NewReplayIterator([]domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
		makeBar(t3, 103),
		makeBar(t4, 104),
	})
	// Advance to t3
	iter.Next()
	iter.Next()
	iter.Next()

	bars, err := iter.BarsInRange(t2, t3)
	if err != nil {
		t.Fatalf("BarsInRange() error = %v", err)
	}
	if len(bars) != 2 {
		t.Fatalf("len(BarsInRange()) = %d, want 2", len(bars))
	}
	if bars[0].Close != 102 || bars[1].Close != 103 {
		t.Fatalf("BarsInRange() = [%f, %f], want [102, 103]", bars[0].Close, bars[1].Close)
	}
}

func TestBarsInRangeBeforeNextReturnsErrNotStarted(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	iter, _ := NewReplayIterator([]domain.OHLCV{makeBar(t1, 100)})

	_, err := iter.BarsInRange(t1, t1)
	if !errors.Is(err, ErrNotStarted) {
		t.Fatalf("BarsInRange() before Next() error = %v, want %v", err, ErrNotStarted)
	}
}

func TestRemainingAndLen(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	t3 := t2.Add(24 * time.Hour)

	iter, _ := NewReplayIterator([]domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
		makeBar(t3, 103),
	})

	if iter.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", iter.Len())
	}
	if iter.Remaining() != 3 {
		t.Fatalf("Remaining() before Next = %d, want 3", iter.Remaining())
	}

	iter.Next()
	if iter.Remaining() != 2 {
		t.Fatalf("Remaining() after 1 Next = %d, want 2", iter.Remaining())
	}

	iter.Next()
	iter.Next()
	if iter.Remaining() != 0 {
		t.Fatalf("Remaining() after all Next = %d, want 0", iter.Remaining())
	}
}

func TestDone(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)

	iter, _ := NewReplayIterator([]domain.OHLCV{
		makeBar(t1, 101),
		makeBar(t2, 102),
	})

	if iter.Done() {
		t.Fatal("Done() = true before any Next")
	}

	iter.Next()
	if iter.Done() {
		t.Fatal("Done() = true after first Next")
	}

	iter.Next()
	if !iter.Done() {
		t.Fatal("Done() = false after all bars consumed")
	}
}

func TestFullReplayLoop(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)
	step := time.Hour

	bars := make([]domain.OHLCV, 0, 5)
	for i := 0; i < 5; i++ {
		bars = append(bars, makeBar(t1.Add(time.Duration(i)*step), float64(100+i)))
	}

	iter, err := NewReplayIterator(bars)
	if err != nil {
		t.Fatalf("NewReplayIterator() error = %v", err)
	}

	step_i := 0
	for iter.Next() {
		current, err := iter.Current()
		if err != nil {
			t.Fatalf("Current() error = %v at step %d", err, step_i)
		}
		if current.Close != float64(100+step_i) {
			t.Fatalf("bar[%d].Close = %f, want %f", step_i, current.Close, float64(100+step_i))
		}

		visible, err := iter.Bars()
		if err != nil {
			t.Fatalf("Bars() error = %v at step %d", err, step_i)
		}
		if len(visible) != step_i+1 {
			t.Fatalf("len(Bars()) = %d at step %d, want %d", len(visible), step_i, step_i+1)
		}

		// Attempting to access future data should fail.
		if step_i < 4 {
			futureTime := bars[step_i+1].Timestamp
			_, err := iter.BarAt(futureTime)
			if !errors.Is(err, ErrFutureAccess) {
				t.Fatalf("BarAt(future) error = %v at step %d, want %v", err, step_i, ErrFutureAccess)
			}
		}

		step_i++
	}

	if step_i != 5 {
		t.Fatalf("consumed %d bars, want 5", step_i)
	}

	if !iter.Done() {
		t.Fatal("Done() = false after full iteration")
	}
}

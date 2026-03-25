package scheduler

import (
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestStrategyDedup_TryAcquire(t *testing.T) {
	var d strategyDedup
	id := uuid.New()

	if !d.TryAcquire(id) {
		t.Fatal("first TryAcquire should succeed")
	}
	if d.TryAcquire(id) {
		t.Fatal("second TryAcquire should fail")
	}
}

func TestStrategyDedup_Release(t *testing.T) {
	var d strategyDedup
	id := uuid.New()

	d.TryAcquire(id)
	d.Release(id)

	if !d.TryAcquire(id) {
		t.Fatal("TryAcquire after Release should succeed")
	}
}

func TestStrategyDedup_DifferentStrategies(t *testing.T) {
	var d strategyDedup
	id1 := uuid.New()
	id2 := uuid.New()

	if !d.TryAcquire(id1) {
		t.Fatal("TryAcquire(id1) should succeed")
	}
	if !d.TryAcquire(id2) {
		t.Fatal("TryAcquire(id2) should succeed when id1 is in-flight")
	}
}

func TestStrategyDedup_Concurrent(t *testing.T) {
	var d strategyDedup
	id := uuid.New()

	const goroutines = 100
	var acquired int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if d.TryAcquire(id) {
				mu.Lock()
				acquired++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if acquired != 1 {
		t.Fatalf("expected exactly 1 acquisition, got %d", acquired)
	}
}

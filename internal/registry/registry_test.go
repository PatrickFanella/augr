package registry_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/registry"
)

func TestRegisterAndGet(t *testing.T) {
	r := registry.New[string, int](nil)
	r.Register("key", 42)

	got, ok := r.Get("key")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got != 42 {
		t.Fatalf("Get() = %d, want 42", got)
	}
}

func TestGetMissing(t *testing.T) {
	r := registry.New[string, int](nil)

	_, ok := r.Get("missing")
	if ok {
		t.Fatal("Get(missing) ok = true, want false")
	}
}

func TestResolve(t *testing.T) {
	r := registry.New[string, string](nil)
	r.Register("k", "v")

	got, err := r.Resolve("k", nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "v" {
		t.Fatalf("Resolve() = %q, want %q", got, "v")
	}
}

func TestResolveMissing(t *testing.T) {
	r := registry.New[string, int](nil)
	sentinel := errors.New("custom not found")

	_, err := r.Resolve("missing", sentinel)
	if err == nil {
		t.Fatal("Resolve() error = nil, want non-nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Resolve() error = %v, want wrapping %v", err, sentinel)
	}
}

func TestResolveMissingNilErr(t *testing.T) {
	r := registry.New[string, int](nil)

	_, err := r.Resolve("missing", nil)
	if err == nil {
		t.Fatal("Resolve() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Resolve() error = %q, want containing %q", err.Error(), "not found")
	}
}

func TestNormalize(t *testing.T) {
	r := registry.New[string, int](strings.ToLower)
	r.Register("KEY", 99)

	got, ok := r.Get("key")
	if !ok {
		t.Fatal("Get(key) ok = false, want true")
	}
	if got != 99 {
		t.Fatalf("Get(key) = %d, want 99", got)
	}

	got, ok = r.Get("Key")
	if !ok {
		t.Fatal("Get(Key) ok = false, want true")
	}
	if got != 99 {
		t.Fatalf("Get(Key) = %d, want 99", got)
	}
}

func TestNilNormalize(t *testing.T) {
	r := registry.New[string, int](nil)
	r.Register("Key", 1)

	_, ok := r.Get("key")
	if ok {
		t.Fatal("Get(key) ok = true, want false (nil normalize should be case-sensitive)")
	}

	got, ok := r.Get("Key")
	if !ok {
		t.Fatal("Get(Key) ok = false, want true")
	}
	if got != 1 {
		t.Fatalf("Get(Key) = %d, want 1", got)
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := registry.New[int, int](nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		i := i
		go func() {
			defer wg.Done()
			r.Register(i, i*2)
		}()
		go func() {
			defer wg.Done()
			r.Get(i)
		}()
	}
	wg.Wait()
}

func TestOverwrite(t *testing.T) {
	r := registry.New[string, string](nil)
	r.Register("k", "first")
	r.Register("k", "second")

	got, ok := r.Get("k")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got != "second" {
		t.Fatalf("Get() = %q, want %q", got, "second")
	}
}

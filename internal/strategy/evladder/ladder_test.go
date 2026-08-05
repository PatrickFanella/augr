package evladder

import (
	"math"
	"testing"
)

func TestCompute_PositiveEVOnly(t *testing.T) {
	r := Compute(DefaultBuckets(), 0.5, 10)
	for _, x := range r {
		if x.Price > 0.5 {
			t.Fatalf("got > fair rung: %+v", x)
		}
	}
}

func TestCompute_OrderedByEVDesc(t *testing.T) {
	r := Compute(BucketCents{10, 20, 30}, 0.5, 10)
	for i := 1; i < len(r); i++ {
		if r[i-1].EV < r[i].EV {
			t.Fatal("not ordered")
		}
	}
}

func TestCompute_EmptyBucketsReturnsNil(t *testing.T) {
	if got := Compute(nil, 0.5, 10); got != nil {
		t.Fatal("expected nil")
	}
}

func TestCompute_InvalidProbReturnsNil(t *testing.T) {
	if Compute(DefaultBuckets(), -0.1, 10) != nil || Compute(DefaultBuckets(), 1.1, 10) != nil || Compute(DefaultBuckets(), math.NaN(), 10) != nil {
		t.Fatal("expected nil")
	}
}

func TestCompute_SizeScalesWithFreq(t *testing.T) {
	r := Compute(BucketCents{49, 45}, 0.5, 10)
	if len(r) != 2 || r[0].Price != 0.45 || r[1].Price != 0.49 || r[1].Size <= r[0].Size {
		t.Fatal("bad size scaling")
	}
}

func TestDefaultBuckets(t *testing.T) {
	b := DefaultBuckets()
	if len(b) != 20 || b[0] != 2 || b[len(b)-1] != 95 {
		t.Fatal("bad buckets")
	}
}

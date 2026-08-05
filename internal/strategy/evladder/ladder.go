package evladder

import (
	"math"
	sort "sort"
)

type BucketCents []int

func DefaultBuckets() BucketCents {
	return BucketCents{2, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75, 80, 85, 90, 95}
}

type LadderRung struct {
	Price        float64
	Size         float64
	EV           float64
	ExpectedFreq float64
}

func Compute(buckets BucketCents, marketProb, baseSize float64) []LadderRung {
	if len(buckets) == 0 || math.IsNaN(marketProb) || marketProb < 0 || marketProb > 1 {
		return nil
	}
	rungs := make([]LadderRung, 0, len(buckets))
	for _, c := range buckets {
		price := float64(c) / 100
		ev := marketProb - price
		if ev <= 0 {
			continue
		}
		freq := 1 - 2*math.Abs(marketProb-price)
		if freq < 0 {
			freq = 0
		}
		if freq > 1 {
			freq = 1
		}
		size := baseSize * freq
		if freq > 0 && size < baseSize*0.05 {
			size = baseSize * 0.05
		}
		rungs = append(rungs, LadderRung{Price: price, Size: size, EV: ev, ExpectedFreq: freq})
	}
	sort.Slice(rungs, func(i, j int) bool { return rungs[i].EV > rungs[j].EV })
	return rungs
}

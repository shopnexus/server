package main

import (
	"math"
)

func minEatingSpeed(piles []int, h int) int {
	mx := 0
	for _, b := range piles {
		if mx < b {
			mx = b
		}
	}

	getHour := func(k int) int {
		hour := 0
		for _, p := range piles {
			hour += int(math.Ceil(float64(p) / float64(k)))
		}
		return hour
	}

	return bs(1, mx, func(k int) int {
		hour := getHour(k)
		if hour <= h {
			return -1
		}

		return +1
	})
}

func bs(left, right int, fn func(mid int) int) int {
	for left <= right {
		mid := left + (right-left)/2
		t := fn(mid)

		if t == 0 {
			return mid
		} else if t > 0 {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}
	return left
}

func main() {

	// piles = [3,6,7,11], h = 8
	minEatingSpeed([]int{3, 6, 7, 11}, 8)
}

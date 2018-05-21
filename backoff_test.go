// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"testing"
	"time"
)

type FakeTime struct {
	timeseries   []int
	lastIndex    int
	durationUnit time.Duration
}

func (f *FakeTime) GetTime() time.Time {
	timeDelta := time.Duration(f.timeseries[f.lastIndex]) * f.durationUnit
	fakeTime := time.Unix(0, 0).Add(timeDelta)
	f.lastIndex++
	return fakeTime
}

func FakeJitter(input int) int {
	return input
}

func RunBackoffTest(t *testing.T,
	maxBackoff float64, resetDelta float64,
	elapsedTime []int, expectedDelays []int) {
	fakeTime := &FakeTime{
		timeseries:   elapsedTime,
		lastIndex:    0,
		durationUnit: time.Millisecond,
	}
	backoff := NewBackoffForTesting(maxBackoff, resetDelta, time.Millisecond,
		FakeJitter, fakeTime.GetTime)

	for i, value := range expectedDelays {
		expected_delay := time.Duration(value) * time.Millisecond
		delay := backoff.GetDelay()
		if expected_delay != delay {
			t.Errorf("Call #%d of GetDelay returned %s (expected %s)",
				i, delay, expected_delay)
		}
	}
}

func TestBackoffIncreasesAndReachesMax(t *testing.T) {
	RunBackoffTest(t,
		8,
		32,
		// Simple sequential time
		[]int{0, 0, 1, 2, 3, 4, 5, 6, 7},
		// Exponential ramp-up to max, then keep max.
		[]int{0, 2, 4, 8, 8, 8, 8, 8},
	)
}

func TestBackoffReset(t *testing.T) {
	RunBackoffTest(t,
		8,
		32,
		// Simulate two intervals bigger than resetDelta
		[]int{0, 0, 1, 2, 50, 51, 100, 101, 102},
		// Delays get reset each time
		[]int{0, 2, 4, 0, 2, 0, 2, 4},
	)
}

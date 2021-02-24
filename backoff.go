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
	"context"
	"log"
	"math"
	"math/rand"
	"time"
)

type JitterFunc func(int) int

type Delayer interface {
	Delay()
	DelayContext(context.Context) bool
}

type Backoff struct {
	step         float64
	maxBackoff   float64
	resetDelta   float64
	lastAttempt  time.Time
	durationUnit time.Duration
	jitterer     JitterFunc
	timeTeller   TimeTeller
}

func jitterFunc(input int) int {
	if input == 0 {
		return 0
	}
	return rand.Intn(input)
}

// TimeTeller interface allows injection of fake time during testing
type TimeTeller interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type RealTime struct{}

func (r *RealTime) Now() time.Time {
	return time.Now()
}

func (r *RealTime) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

func NewBackoff(maxBackoff float64, resetDelta float64,
	durationUnit time.Duration) *Backoff {
	timeTeller := &RealTime{}
	return NewBackoffForTesting(
		maxBackoff, resetDelta, durationUnit, jitterFunc, timeTeller)
}

func NewBackoffForTesting(maxBackoff float64, resetDelta float64,
	durationUnit time.Duration, jitterer JitterFunc, timeTeller TimeTeller) *Backoff {
	return &Backoff{
		step:         0,
		maxBackoff:   maxBackoff,
		resetDelta:   resetDelta,
		lastAttempt:  timeTeller.Now(),
		durationUnit: durationUnit,
		jitterer:     jitterer,
		timeTeller:   timeTeller,
	}
}

func (b *Backoff) maybeReset() {
	now := b.timeTeller.Now()
	lastAttemptDelta := float64(now.Sub(b.lastAttempt) / b.durationUnit)
	b.lastAttempt = now

	if lastAttemptDelta >= b.resetDelta {
		b.step = 0
	}
}

func (b *Backoff) GetDelay() time.Duration {
	b.maybeReset()

	var synchronizedDuration float64
	// Do not add any delay the first time.
	if b.step == 0 {
		synchronizedDuration = 0
	} else {
		synchronizedDuration = math.Pow(2, b.step)
	}

	if synchronizedDuration < b.maxBackoff {
		b.step++
	} else {
		synchronizedDuration = b.maxBackoff
	}
	duration := time.Duration(b.jitterer(int(synchronizedDuration)))
	return duration * b.durationUnit
}

func (b *Backoff) Delay() {
	b.DelayContext(context.Background())
}

func (b *Backoff) DelayContext(ctx context.Context) bool {
	delay := b.GetDelay()
	log.Printf("Backoff for %s starts", delay)
	select {
	case <-b.timeTeller.After(delay):
		log.Printf("Backoff for %s ends", delay)
	case <-ctx.Done():
		log.Printf("Backoff for %s canceled by context", delay)
		return false
	}
	return true
}

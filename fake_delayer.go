// Copyright 2021 Google LLC
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
	"time"
)

type FakeDelayerMaker struct{}

func (fdm *FakeDelayerMaker) NewDelayer(_ float64, _ float64, _ time.Duration) Delayer {
	return &FakeDelayer{
		DelayOnChan: false,
		StopDelay:   make(chan bool),
	}
}

type FakeDelayer struct {
	DelayOnChan bool
	StopDelay   chan bool
}

func (f *FakeDelayer) Delay() {
	f.DelayContext(context.Background())
}

func (f *FakeDelayer) DelayContext(ctx context.Context) bool {
	log.Printf("Faking Backoff")
	if f.DelayOnChan {
		log.Printf("Waiting StopDelay signal")
		<-f.StopDelay
		log.Printf("Received StopDelay signal")
	}
	return true
}

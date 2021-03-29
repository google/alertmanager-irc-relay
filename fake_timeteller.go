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
	"time"
)

type FakeTime struct {
	timeseries   []int
	lastIndex    int
	durationUnit time.Duration
	afterChan    chan time.Time
}

func (f *FakeTime) Now() time.Time {
	timeDelta := time.Duration(f.timeseries[f.lastIndex]) * f.durationUnit
	fakeTime := time.Unix(0, 0).Add(timeDelta)
	f.lastIndex++
	return fakeTime
}

func (f *FakeTime) After(d time.Duration) <-chan time.Time {
	return f.afterChan
}

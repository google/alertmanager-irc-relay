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
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type FakeHTTPListener struct {
	StartedServing chan bool
	StopServing    chan bool
	AlertMsgs      chan AlertMsg // kinda ugly putting it here, but convenient
	router         http.Handler
}

func (listener *FakeHTTPListener) Serve(_ string, router http.Handler) error {
	listener.router = router

	listener.StartedServing <- true
	<-listener.StopServing
	return nil
}

func NewFakeHTTPListener() *FakeHTTPListener {
	return &FakeHTTPListener{
		StartedServing: make(chan bool),
		StopServing:    make(chan bool),
		AlertMsgs:      make(chan AlertMsg, 10),
	}
}

func MakeHTTPTestingConfig() *Config {
	return &Config{
		HTTPHost:    "test.web",
		HTTPPort:    8888,
		MsgTemplate: "Alert {{ .Labels.alertname }} on {{ .Labels.instance }} is {{ .Status }}",
	}
}

func RunHTTPTest(t *testing.T,
	alertData string, url string,
	testingConfig *Config, listener *FakeHTTPListener) *http.Response {
	httpServer, err := NewHTTPServerForTesting(testingConfig,
		listener.AlertMsgs, listener.Serve)
	if err != nil {
		t.Fatal(fmt.Sprintf("Could not create HTTP server: %s", err))
	}

	go httpServer.Run()

	<-listener.StartedServing

	alertDataReader := strings.NewReader(alertData)
	request, err := http.NewRequest("POST", url, alertDataReader)
	if err != nil {
		t.Fatal(fmt.Sprintf("Could not create HTTP request: %s", err))
	}
	responseRecorder := httptest.NewRecorder()

	listener.router.ServeHTTP(responseRecorder, request)

	listener.StopServing <- true
	<-httpServer.StoppedRunning
	return responseRecorder.Result()
}

func TestAlertsDispatched(t *testing.T) {
	listener := NewFakeHTTPListener()
	testingConfig := MakeHTTPTestingConfig()

	expectedAlertMsgs := []AlertMsg{
		AlertMsg{
			Channel: "#somechannel",
			Alert:   "Alert airDown on instance1:3456 is resolved",
		},
		AlertMsg{
			Channel: "#somechannel",
			Alert:   "Alert airDown on instance2:7890 is resolved",
		},
	}
	expectedStatusCode := 200

	response := RunHTTPTest(
		t, testdataSimpleAlertJson, "/somechannel",
		testingConfig, listener)

	if expectedStatusCode != response.StatusCode {
		t.Error(fmt.Sprintf("Expected %d status in response, got %d",
			expectedStatusCode, response.StatusCode))
	}

	for _, expectedAlertMsg := range expectedAlertMsgs {
		alertMsg := <-listener.AlertMsgs
		if !reflect.DeepEqual(expectedAlertMsg, alertMsg) {
			t.Error(fmt.Sprintf(
				"Unexpected alert msg.\nExpected: %s\nActual: %s",
				expectedAlertMsg, alertMsg))
		}
	}
}

func TestRootReturnsError(t *testing.T) {
	listener := NewFakeHTTPListener()
	testingConfig := MakeHTTPTestingConfig()

	expectedStatusCode := 404

	response := RunHTTPTest(
		t, testdataSimpleAlertJson, "/",
		testingConfig, listener)

	if expectedStatusCode != response.StatusCode {
		t.Error(fmt.Sprintf("Expected %d status in response, got %d",
			expectedStatusCode, response.StatusCode))
	}
}

func TestInvalidDataReturnsError(t *testing.T) {
	listener := NewFakeHTTPListener()
	testingConfig := MakeHTTPTestingConfig()

	expectedStatusCode := 422

	response := RunHTTPTest(
		t, testdataBogusAlertJson, "/somechannel",
		testingConfig, listener)

	if expectedStatusCode != response.StatusCode {
		t.Error(fmt.Sprintf("Expected %d status in response, got %d",
			expectedStatusCode, response.StatusCode))
	}
}

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
	AlertNotices   chan AlertNotice // kinda ugly putting it here, but convenient
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
		AlertNotices:   make(chan AlertNotice, 10),
	}
}

func MakeHTTPTestingConfig() *Config {
	return &Config{
		HTTPHost:       "test.web",
		HTTPPort:       8888,
		NoticeTemplate: "Alert {{ .Labels.alertname }} on {{ .Labels.instance }} is {{ .Status }}",
	}
}

func RunHTTPTest(t *testing.T,
	alertData string, url string,
	testingConfig *Config, listener *FakeHTTPListener) *http.Response {
	httpServer, err := NewHTTPServerForTesting(testingConfig,
		listener.AlertNotices, listener.Serve)
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

	expectedAlertNotices := []AlertNotice{
		AlertNotice{
			Channel: "#somechannel",
			Alert:   "Alert airDown on instance1:3456 is resolved",
		},
		AlertNotice{
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

	for _, expectedAlertNotice := range expectedAlertNotices {
		alertNotice := <-listener.AlertNotices
		if !reflect.DeepEqual(expectedAlertNotice, alertNotice) {
			t.Error(fmt.Sprintf(
				"Unexpected alert notice.\nExpected: %s\nActual: %s",
				expectedAlertNotice, alertNotice))
		}
	}
}

func TestAlertsDispatchedOnce(t *testing.T) {
	listener := NewFakeHTTPListener()
	testingConfig := MakeHTTPTestingConfig()
	testingConfig.NoticeOnce = true
	testingConfig.NoticeTemplate = "Alert {{ .GroupLabels.alertname }} is {{ .Status }}"

	expectedAlertNotices := []AlertNotice{
		AlertNotice{
			Channel: "#somechannel",
			Alert:   "Alert airDown is resolved",
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

	for _, expectedAlertNotice := range expectedAlertNotices {
		alertNotice := <-listener.AlertNotices
		if !reflect.DeepEqual(expectedAlertNotice, alertNotice) {
			t.Error(fmt.Sprintf(
				"Unexpected alert notice.\nExpected: %s\nActual: %s",
				expectedAlertNotice, alertNotice))
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

func TestTemplateErrorsCreateRawAlertNotice(t *testing.T) {
	listener := NewFakeHTTPListener()
	testingConfig := MakeHTTPTestingConfig()
	testingConfig.NoticeTemplate = "Bogus template {{ nil }}"

	expectedAlertNotices := []AlertNotice{
		AlertNotice{
			Channel: "#somechannel",
			Alert:   `{"status":"resolved","labels":{"alertname":"airDown","instance":"instance1:3456","job":"air","service":"prometheus","severity":"ticket","zone":"global"},"annotations":{"DESCRIPTION":"service /prometheus has irc gateway down on instance1","SUMMARY":"service /prometheus air down on instance1"},"startsAt":"2017-05-15T13:49:37.834Z","endsAt":"2017-05-15T13:50:37.835Z","generatorURL":"https://prometheus.example.com/prometheus/..."}`,
		},
		AlertNotice{
			Channel: "#somechannel",
			Alert:   `{"status":"resolved","labels":{"alertname":"airDown","instance":"instance2:7890","job":"air","service":"prometheus","severity":"ticket","zone":"global"},"annotations":{"DESCRIPTION":"service /prometheus has irc gateway down on instance2","SUMMARY":"service /prometheus air down on instance2"},"startsAt":"2017-05-15T11:47:37.834Z","endsAt":"2017-05-15T11:48:37.834Z","generatorURL":"https://prometheus.example.com/prometheus/..."}`,
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

	for _, expectedAlertNotice := range expectedAlertNotices {
		alertNotice := <-listener.AlertNotices
		if !reflect.DeepEqual(expectedAlertNotice, alertNotice) {
			t.Error(fmt.Sprintf(
				"Unexpected alert notice.\nExpected: %s\nActual: %s",
				expectedAlertNotice, alertNotice))
		}
	}
}

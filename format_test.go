// Copyright 2020 Google LLC
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
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	promtmpl "github.com/prometheus/alertmanager/template"
)

func CreateFormatterAndCheckOutput(t *testing.T, c *Config, expected []AlertMsg) {
	f, _ := NewFormatter(c)

	var alertMessage = promtmpl.Data{}
	if err := json.Unmarshal([]byte(testdataSimpleAlertJson), &alertMessage); err != nil {
		t.Fatal(fmt.Sprintf("Could not unmarshal %s", testdataSimpleAlertJson))
	}

	alertMsgs := f.GetMsgsFromAlertMessage("#somechannel", &alertMessage)

	if !reflect.DeepEqual(expected, alertMsgs) {
		t.Error(fmt.Sprintf(
			"Unexpected alert msg.\nExpected: %s\nActual: %s",
			expected, alertMsgs))

	}

}

func TestTemplateErrorsCreateRawAlertMsg(t *testing.T) {
	testingConfig := Config{MsgTemplate: "Bogus template {{ nil }}"}

	expectedAlertMsgs := []AlertMsg{
		AlertMsg{
			Channel: "#somechannel",
			Alert:   `{"status":"resolved","labels":{"alertname":"airDown","instance":"instance1:3456","job":"air","service":"prometheus","severity":"ticket","zone":"global"},"annotations":{"DESCRIPTION":"service /prometheus has irc gateway down on instance1","SUMMARY":"service /prometheus air down on instance1"},"startsAt":"2017-05-15T13:49:37.834Z","endsAt":"2017-05-15T13:50:37.835Z","generatorURL":"https://prometheus.example.com/prometheus/...","fingerprint":"66214a361160fb6f"}`,
		},
		AlertMsg{
			Channel: "#somechannel",
			Alert:   `{"status":"resolved","labels":{"alertname":"airDown","instance":"instance2:7890","job":"air","service":"prometheus","severity":"ticket","zone":"global"},"annotations":{"DESCRIPTION":"service /prometheus has irc gateway down on instance2","SUMMARY":"service /prometheus air down on instance2"},"startsAt":"2017-05-15T11:47:37.834Z","endsAt":"2017-05-15T11:48:37.834Z","generatorURL":"https://prometheus.example.com/prometheus/...","fingerprint":"25a874c99325d1ce"}`,
		},
	}

	CreateFormatterAndCheckOutput(t, &testingConfig, expectedAlertMsgs)
}

func TestAlertsDispatchedOnce(t *testing.T) {
	testingConfig := Config{
		MsgTemplate: "Alert {{ .GroupLabels.alertname }} is {{ .Status }}",
		MsgOnce:     true,
	}

	expectedAlertMsgs := []AlertMsg{
		AlertMsg{
			Channel: "#somechannel",
			Alert:   "Alert airDown is resolved",
		},
	}

	CreateFormatterAndCheckOutput(t, &testingConfig, expectedAlertMsgs)
}

func TestStringsFunctions(t *testing.T) {
	testingConfig := Config{
		MsgTemplate: "Alert {{ .GroupLabels.alertname | ToUpper }} is {{ .Status }}",
		MsgOnce:     true,
	}

	expectedAlertMsgs := []AlertMsg{
		AlertMsg{
			Channel: "#somechannel",
			Alert:   "Alert AIRDOWN is resolved",
		},
	}

	CreateFormatterAndCheckOutput(t, &testingConfig, expectedAlertMsgs)
}

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

const (
	testdataSimpleAlertJson = `
{
    "status": "resolved",
    "receiver": "example_receiver",
    "groupLabels": {
        "alertname": "airDown",
        "service": "prometheus"
    },
    "commonLabels": {
        "alertname": "airDown",
        "job": "air",
        "service": "prometheus",
        "severity": "ticket",
        "zone": "global"
    },
    "commonAnnotations": {},
    "externalURL": "https://prometheus.example.com/alertmanager",
    "alerts": [
        {
            "annotations": {
                "SUMMARY": "service /prometheus air down on instance1",
                "DESCRIPTION": "service /prometheus has irc gateway down on instance1"
            },
            "endsAt": "2017-05-15T13:50:37.835Z",
            "generatorURL": "https://prometheus.example.com/prometheus/...",
	    "fingerprint": "66214a361160fb6f",
            "labels": {
                "alertname": "airDown",
                "instance": "instance1:3456",
                "job": "air",
                "service": "prometheus",
                "severity": "ticket",
                "zone": "global"
            },
            "startsAt": "2017-05-15T13:49:37.834Z",
            "status": "resolved"
        },
        {
            "annotations": {
                "SUMMARY": "service /prometheus air down on instance2",
                "DESCRIPTION": "service /prometheus has irc gateway down on instance2"
            },
            "endsAt": "2017-05-15T11:48:37.834Z",
            "generatorURL": "https://prometheus.example.com/prometheus/...",
	    "fingerprint": "25a874c99325d1ce",
            "labels": {
                "alertname": "airDown",
                "instance": "instance2:7890",
                "job": "air",
                "service": "prometheus",
                "severity": "ticket",
                "zone": "global"
            },
            "startsAt": "2017-05-15T11:47:37.834Z",
            "status": "resolved"
        }
    ]
}
`

	testdataBogusAlertJson = `{"this is not": "a valid alert",}`
)

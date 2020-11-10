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
	"bytes"
	"encoding/json"
	"log"
	"net/url"
	"strings"
	"text/template"

	promtmpl "github.com/prometheus/alertmanager/template"
)

type Formatter struct {
	MsgTemplate *template.Template
	MsgOnce     bool
}

func NewFormatter(config *Config) (*Formatter, error) {
	funcMap := template.FuncMap{
		"ToUpper": strings.ToUpper,
		"ToLower": strings.ToLower,
		"Join":    strings.Join,

		"QueryEscape": url.QueryEscape,
		"PathEscape":  url.PathEscape,
	}

	tmpl, err := template.New("msg").Funcs(funcMap).Parse(config.MsgTemplate)
	if err != nil {
		return nil, err
	}
	return &Formatter{
		MsgTemplate: tmpl,
		MsgOnce:     config.MsgOnce,
	}, nil
}

func (f *Formatter) FormatMsg(ircChannel string, data interface{}) string {
	output := bytes.Buffer{}
	var msg string
	if err := f.MsgTemplate.Execute(&output, data); err != nil {
		msg_bytes, _ := json.Marshal(data)
		msg = string(msg_bytes)
		log.Printf("Could not apply msg template on alert (%s): %s",
			err, msg)
		log.Printf("Sending raw alert")
		alertHandlingErrors.WithLabelValues(ircChannel, "format_msg").Inc()
	} else {
		msg = output.String()
	}
	return msg
}

func (f *Formatter) GetMsgsFromAlertMessage(ircChannel string,
	data *promtmpl.Data) []AlertMsg {
	msgs := []AlertMsg{}
	if f.MsgOnce {
		msg := f.FormatMsg(ircChannel, data)
		msgs = append(msgs,
			AlertMsg{Channel: ircChannel, Alert: msg})
	} else {
		for _, alert := range data.Alerts {
			msg := f.FormatMsg(ircChannel, alert)
			msgs = append(msgs,
				AlertMsg{Channel: ircChannel, Alert: msg})
		}
	}
	return msgs
}

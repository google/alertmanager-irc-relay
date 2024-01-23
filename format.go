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
	"net/url"
	"strings"
	"text/template"

	"github.com/google/alertmanager-irc-relay/logging"
	promtmpl "github.com/prometheus/alertmanager/template"
)

type Formatter struct {
	MsgTemplate *template.Template
	MsgTemplateResolved *template.Template
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
	tmplresolved, err := template.New("msg").Funcs(funcMap).Parse(config.MsgTemplateResolved)
	if err != nil {
		return nil, err
	}
	return &Formatter{
		MsgTemplate: tmpl,
		MsgTemplateResolved: tmplresolved,
		MsgOnce:     config.MsgOnce,
	}, nil
}

func (f *Formatter) FormatMsg(ircChannel string, data interface{}, status string) []string {
	output := bytes.Buffer{}
	var msg string
	var msgtemplate *template.Template
	if status == "resolved" {
		msgtemplate = f.MsgTemplateResolved
	} else {
		msgtemplate = f.MsgTemplate
	}
	if err := msgtemplate.Execute(&output, data); err != nil {
		msg_bytes, _ := json.Marshal(data)
		msg = string(msg_bytes)
		logging.Error("Could not apply msg template on alert (%s): %s",
			err, msg)
		logging.Warn("Sending raw alert")
		alertHandlingErrors.WithLabelValues(ircChannel, "format_msg").Inc()
	} else {
		msg = output.String()
	}

	// Do not send to IRC messages with newlines, split in multiple messages instead.
	newLinesSplit := func(r rune) bool {
		return r == '\n' || r == '\r'
	}
	return strings.FieldsFunc(msg, newLinesSplit)
}

func (f *Formatter) GetMsgsFromAlertMessage(ircChannel string,
	data *promtmpl.Data) []AlertMsg {
	msgs := []AlertMsg{}
	if f.MsgOnce {
		for _, msg := range f.FormatMsg(ircChannel, data, data.Status) {
			msgs = append(msgs,
				AlertMsg{Channel: ircChannel, Alert: msg})
		}
	} else {
		for _, alert := range data.Alerts {
			for _, msg := range f.FormatMsg(ircChannel, alert, data.Status) {
				msgs = append(msgs,
					AlertMsg{Channel: ircChannel, Alert: msg})
			}
		}
	}
	return msgs
}

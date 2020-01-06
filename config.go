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
	"gopkg.in/yaml.v2"
	"io/ioutil"
)

const (
	defaultNoticeOnceTemplate = "Alert {{ .GroupLabels.alertname }} for {{ .GroupLabels.job }} is {{ .Status }}"
	defaultNoticeTemplate     = "Alert {{ .Labels.alertname }} on {{ .Labels.instance }} is {{ .Status }}"
)

type IRCChannel struct {
	Name     string `yaml:"name"`
	Password string `yaml:"password"`
}

type Config struct {
	HTTPHost       string       `yaml:"http_host"`
	HTTPPort       int          `yaml:"http_port"`
	IRCNick        string       `yaml:"irc_nickname"`
	IRCNickPass    string       `yaml:"irc_nickname_password"`
	IRCRealName    string       `yaml:"irc_realname"`
	IRCHost        string       `yaml:"irc_host"`
	IRCPort        int          `yaml:"irc_port"`
	IRCUseSSL      bool         `yaml:"irc_use_ssl"`
	IRCChannels    []IRCChannel `yaml:"irc_channels"`
	NoticeTemplate string       `yaml:"notice_template"`
	NoticeOnce     bool         `yaml:"notice_once_per_alert_group"`
	UsePrivmsg     bool         `yaml:"use_privmsg"`
}

func LoadConfig(configFile string) (*Config, error) {
	config := &Config{
		HTTPHost:    "localhost",
		HTTPPort:    8000,
		IRCNick:     "alertmanager-irc-relay",
		IRCNickPass: "",
		IRCRealName: "Alertmanager IRC Relay",
		IRCHost:     "irc.freenode.net",
		IRCPort:     7000,
		IRCUseSSL:   true,
		IRCChannels: []IRCChannel{IRCChannel{Name: "#airtest"}},
		NoticeOnce:  false,
		UsePrivmsg:  false,
	}

	if configFile != "" {
		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, err
		}
	}

	// Set default template if config does not have one.
	if config.NoticeTemplate == "" {
		if config.NoticeOnce {
			config.NoticeTemplate = defaultNoticeOnceTemplate
		} else {
			config.NoticeTemplate = defaultNoticeTemplate
		}
	}

	return config, nil
}

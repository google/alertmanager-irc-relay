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
	"os"

	"github.com/google/alertmanager-irc-relay/logging"
)

const (
	defaultMsgOnceTemplate = "Alert {{ .GroupLabels.alertname }} for {{ .GroupLabels.job }} is {{ .Status }}"
	defaultMsgTemplate     = "Alert {{ .Labels.alertname }} on {{ .Labels.instance }} is {{ .Status }}"
)

type IRCChannel struct {
	Name     string `yaml:"name"`
	Password string `yaml:"password"`
}

type Config struct {
	HTTPHost        string       `yaml:"http_host"`
	HTTPPort        int          `yaml:"http_port"`
	IRCNick         string       `yaml:"irc_nickname"`
	IRCNickPass     string       `yaml:"irc_nickname_password"`
	IRCRealName     string       `yaml:"irc_realname"`
	IRCHost         string       `yaml:"irc_host"`
	IRCPort         int          `yaml:"irc_port"`
	IRCHostPass     string       `yaml:"irc_host_password"`
	IRCUseSSL       bool         `yaml:"irc_use_ssl"`
	IRCVerifySSL    bool         `yaml:"irc_verify_ssl"`
	IRCChannels     []IRCChannel `yaml:"irc_channels"`
	MsgTemplate     string       `yaml:"msg_template"`
	MsgOnce         bool         `yaml:"msg_once_per_alert_group"`
	UsePrivmsg      bool         `yaml:"use_privmsg"`
	AlertBufferSize int          `yaml:"alert_buffer_size"`

	NickservName    string       `yaml:"nickserv_name"`
	NickservIdentifyPatterns []string `yaml:"nickserv_identify_patterns"`
	ChanservName    string       `yaml:"chanserv_name"`
}

func LoadConfig(configFile string) (*Config, error) {
	config := &Config{
		HTTPHost:        "localhost",
		HTTPPort:        8000,
		IRCNick:         "alertmanager-irc-relay",
		IRCNickPass:     "",
		IRCRealName:     "Alertmanager IRC Relay",
		IRCHost:         "example.com",
		IRCPort:         7000,
		IRCHostPass:     "",
		IRCUseSSL:       true,
		IRCVerifySSL:    true,
		IRCChannels:     []IRCChannel{},
		MsgOnce:         false,
		UsePrivmsg:      false,
		AlertBufferSize: 2048,
		NickservName:    "NickServ",
		NickservIdentifyPatterns: []string{
			"Please choose a different nickname, or identify via",
			"identify via /msg NickServ identify <password>",
			"type /msg NickServ IDENTIFY password",
			"authenticate yourself to services with the IDENTIFY command",
		},
		ChanservName:    "ChanServ",
	}

	if configFile != "" {
		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			return nil, err
		}
		data = []byte(os.ExpandEnv(string(data)))
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, err
		}
	}

	// Set default template if config does not have one.
	if config.MsgTemplate == "" {
		if config.MsgOnce {
			config.MsgTemplate = defaultMsgOnceTemplate
		} else {
			config.MsgTemplate = defaultMsgTemplate
		}
	}

	loadedConfig, _ := yaml.Marshal(config)
	logging.Debug("Loaded config:\n%s", loadedConfig)

	return config, nil
}

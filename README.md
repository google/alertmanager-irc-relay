# Alertmanager IRC Relay

Alertmanager IRC Relay is a bot relaying [Prometheus](https://prometheus.io/) alerts to IRC.
Alerts are received from Prometheus using
[Webhooks](https://prometheus.io/docs/alerting/configuration/#webhook-receiver-<webhook_config>)
and are relayed to an IRC channel.

### Configuring and running the bot

To configure and run the bot you need to create a YAML configuration file and
pass it to the service. Running the service without a configuration will use
the default test values and connect to a default IRC channel, which you
probably do not want to do.

Example configuration:
```
# Start the HTTP server receiving alerts from Prometheus Webhook binding to
# this host/port.
#
http_host: localhost
http_port: 8000

# Connect to this IRC host/port.
#
# Note: SSL is enabled by default, use "irc_use_ssl: no" to disable.
irc_host: irc.example.com
irc_port: 7000

# Use this IRC nickname.
irc_nickname: myalertbot
# Password used to identify with NickServ
irc_nickname_password: mynickserv_key
# Use this IRC real name
irc_realname: myrealname

# Optionally pre-join certain channels.
#
# Note: If an alert is sent to a non # pre-joined channel the bot will join
# that channel anyway before sending the message. Of course this cannot work
# with password-protected channels.
irc_channels:
  - name: "#mychannel"
  - name: "#myprivatechannel"
    password: myprivatechannel_key

# Define how IRC messages should be sent.
#
# Send only one message when webhook data is received.
# Note: By default a message is sent for each alert in the webhook data.
msg_once_per_alert_group: no

# Define how IRC messages should be formatted.
#
# The formatting is based on golang's text/template .
msg_template: "Alert {{ .Labels.alertname }} on {{ .Labels.instance }} is {{ .Status }}"
# Note: When sending only one message per alert group the default
# msg_template is set to
# "Alert {{ .GroupLabels.alertname }} for {{ .GroupLabels.job }} is {{ .Status }}"
```

Running the bot (assuming *$GOPATH* and *$PATH* are properly setup for go):
```
$ go install github.com/google/alertmanager-irc-relay
$ alertmanager-irc-relay --config /path/to/your/config/file
```

### Prometheus configuration

Prometheus can be configured following the official
[Webhooks](https://prometheus.io/docs/alerting/configuration/#webhook-receiver-<webhook_config>)
documentation. The `url` must specify the IRC channel name that alerts should
be sent to:
```
send_resolved: false
url: http://localhost:8000/mychannel
```



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
# Set "irc_verify_ssl: no" to accept invalid SSL certificates (not recommended)
irc_host: irc.example.com
irc_port: 7000
# Optionally set the server password
irc_host_password: myserver_password

# Use this IRC nickname.
irc_nickname: myalertbot
# Password used to identify with NickServ
irc_nickname_password: mynickserv_key
# If true, SASL authentication will be used to log in to the server.
irc_use_sasl: false
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
#
# Use PRIVMSG instead of NOTICE (default) to send messages.
# Note: Sending PRIVMSG from bots is bad practice, do not enable this unless
# necessary (e.g. unless NOTICEs would weaken your channel moderation policies)
use_privmsg: yes

# Define how IRC messages should be formatted.
#
# The formatting is based on golang's text/template .
msg_template: "Alert {{ .Labels.alertname }} on {{ .Labels.instance }} is {{ .Status }}"
# Note: When sending only one message per alert group the default
# msg_template is set to
# "Alert {{ .GroupLabels.alertname }} for {{ .GroupLabels.job }} is {{ .Status }}"

# Set the internal buffer size for alerts received but not yet sent to IRC.
alert_buffer_size: 2048

# Patterns used to guess whether NickServ is asking us to IDENTIFY
# Note: If you need to change this because the bot is not catching a request
# from a rather common NickServ, please consider sending a PR to update the
# default config instead.
nickserv_identify_patterns:
  - "identify via /msg NickServ identify <password>"
  - "type /msg NickServ IDENTIFY password"
  - "authenticate yourself to services with the IDENTIFY command"

# Rarely NickServ or ChanServ is reached at a specific hostname.  Specify an
# override here
nickserv_name: NickServ
chanserv_name: ChanServ
```

Running the bot (assuming *$GOPATH* and *$PATH* are properly setup for go):
```
$ go install github.com/google/alertmanager-irc-relay
$ alertmanager-irc-relay --config /path/to/your/config/file
```

The configuration file can reference environment variables. It is then possible
to specify certain parameters directly when running the bot:
```
$ cat /path/to/your/config/file
...
http_port: $MY_SERVICE_PORT
...
irc_nickname_password: $NICKSERV_PASSWORD
...
$ export MY_SERVICE_PORT=8000 NICKSERV_PASSWORD=mynickserv_key
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

# FluxCD Bot

`fluxcdbot` is a Telergram bot for [Flux](https://github.com/fluxcd/flux2).
`fluxcdbot` accepts webhooks from the [Flux notification-controller](https://github.com/fluxcd/notification-controller) and forwards the messages to corresponding Telegram chat.

[![Build Status](https://github.com/squat/fluxcdbot/workflows/CI/badge.svg)](https://github.com/squat/fluxcdbot/actions?query=workflow%3ACI)
[![Go Report Card](https://goreportcard.com/badge/github.com/squat/fluxcdbot)](https://goreportcard.com/report/github.com/squat/fluxcdbot)

## Overview

In order to subscribe to Telegram messages from the bot, send `/start` to the bot via chat.
The bot will in turn respond with a unique webhook URL specific to this chat.
The given webhook URL can then be configured with the notification-controller using the `Provider` and `Alert` custom resources.
For example, the following snippet could be used to forward all updates to the chat:

```shell
cat <<'EOF' | kubectl apply -f -
apiVersion: notification.toolkit.fluxcd.io/v1beta1
kind: Provider
metadata:
  name: fluxcdbot
spec:
  type: generic
  secretRef:
    name: fluxcdbot
---
apiVersion: v1
kind: Secret
metadata:
  name: fluxcdbot
stringData:
  address: <webhook-URL>
---
apiVersion: notification.toolkit.fluxcd.io/v1beta1
kind: Alert
metadata:
  name: fluxcdbot
spec:
  providerRef:
    name: fluxcdbot
  eventSeverity: info
  eventSources:
  - kind: Kustomization
    name: flux-system
    namespace: flux-system
EOF
```

Note, webhook URLs should be treated as secrets as anyone with access to the URL can make requests to the bot, which will then send messages to the URL's corresponding chat.
Webhook URLs can be rotated by sending `/rotate` to the bot.

## Installation

In order to deploy `fluxcdbot` to a Kubernetes cluster, first generate a Telegram bot API token using the [BotFather](https://t.me/botfather).
Next, create a Kubernetes Secret for the bot containing this token, e.g.:

```shell
kubectl create secret generic fluxcdbot --from-literal=token=<telegram-token>
```

Finally, deploy the example manifest included in this repository, modifying the base URL as needed:

```shell
kubectl apply -f https://raw.githubusercontent.com/squat/fluxcdbot/master/manifest.yaml
```

## Usage

[embedmd]:# (tmp/help.txt)
```txt
Usage of bin/linux/amd64/fluxcdbot:
  -database string
    	The path to the directory to use for the database. (default "/var/fluxcdbot")
  -listen string
    	The address at which to listen. (default ":8080")
  -listen-internal string
    	The address at which to listen for health and metrics. (default ":9090")
  -log-level string
    	Log level to use. Possible values: all, debug, info, warn, error, none (default "info")
  -tmp string
    	The path to a directory to use for temporary storage. (default "/tmp/fluxcdbot")
  -token string
    	The Telegram API token.
  -url string
    	The URL clients should use to commincate with this server. (default "http://127.0.0.1:8080")
  -version
    	Print version and exit
```

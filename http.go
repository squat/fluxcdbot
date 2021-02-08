// Copyright 2021 the fluxcdbot authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
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
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/fluxcd/pkg/runtime/events"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/google/uuid"
	"github.com/julienschmidt/httprouter"
	"github.com/peterbourgon/diskv/v3"
	"gopkg.in/tucnak/telebot.v2"
)

const webhookEndpoint = "/api/v1/webhook"

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func joinURLPath(a *url.URL, b string) (path, rawpath string) {
	if a.RawPath == "" && b == "" {
		return singleJoiningSlash(a.Path, b), ""
	}
	apath := a.EscapedPath()
	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a.Path + b[1:], apath + b[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b, apath + "/" + b
	}
	return a.Path + b, apath + b
}

func escapeTelegam(u string) string {
	return strings.ReplaceAll(strings.ReplaceAll(u, ".", `\.`), "-", `\-`)
}

func generateURL(baseURL *url.URL, path string) string {
	u := *baseURL
	u.Path, u.RawPath = joinURLPath(&u, path)
	return u.String()
}

func handleStart(d *diskv.Diskv, b *telebot.Bot, baseURL *url.URL, l log.Logger) func(*telebot.Message) {
	return func(m *telebot.Message) {
		chatID := strconv.FormatInt(m.Chat.ID, 10)
		if d.Has(chatID) {
			return
		}
		u := uuid.NewString()
		if err := d.WriteString(chatID, u); err != nil {
			level.Error(l).Log("msg", "failed to write to database", "err", err.Error())
			return
		}
		wu := generateURL(baseURL, path.Join(webhookEndpoint, chatID, u))
		if _, err := b.Send(m.Chat, fmt.Sprintf("Your webhook URL is:\n[%s](%s)", escapeTelegam(wu), wu)); err != nil {
			level.Error(l).Log("msg", "failed to send message", "err", err.Error())
			return
		}
	}
}

func handleRotate(d *diskv.Diskv, b *telebot.Bot, baseURL *url.URL, l log.Logger) func(*telebot.Message) {
	return func(m *telebot.Message) {
		chatID := strconv.FormatInt(m.Chat.ID, 10)
		if !d.Has(chatID) {
			level.Info(l).Log("msg", "this chat is not yet initialized")
			if _, err := b.Send(m.Chat, escapeTelegam("This chat is not yet initialized. Send /start to start receiving updates.")); err != nil {
				level.Error(l).Log("msg", "failed to send message", "err", err.Error())
				return
			}
			return
		}
		u := uuid.NewString()
		if err := d.WriteString(chatID, u); err != nil {
			level.Error(l).Log("msg", "failed to write to database", "err", err.Error())
			return
		}
		wu := generateURL(baseURL, path.Join(webhookEndpoint, chatID, u))
		if _, err := b.Send(m.Chat, fmt.Sprintf("Your new webhook URL is:\n[%s](%s)", escapeTelegam(wu), wu)); err != nil {
			level.Error(l).Log("msg", "failed to send message", "err", err.Error())
			return
		}
	}
}

func handleWebhook(d *diskv.Diskv, b *telebot.Bot) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		params := httprouter.ParamsFromContext(r.Context())
		chatID := params.ByName("chatID")
		cid, err := strconv.ParseInt(chatID, 10, 64)
		if err != nil {
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		if d.ReadString(chatID) != params.ByName("uuid") {
			http.Error(w, "", http.StatusForbidden)
			return
		}
		var e events.Event
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := b.Send(telebot.ChatID(cid), escapeTelegam(fmt.Sprintf("Severity: *%s*\nInstance: *%s*\nMessage:\n```\n%s\n```", e.Severity, e.ReportingInstance, e.Message))); err != nil {
			if apierr, ok := err.(*telebot.APIError); ok {
				http.Error(w, apierr.Description, apierr.Code)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

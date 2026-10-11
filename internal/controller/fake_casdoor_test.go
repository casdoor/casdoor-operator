// Copyright 2026 The Casdoor Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/casdoor/casdoor-operator/internal/casdoor"
)

const (
	fakeClientId      = "fake-client-id"
	fakeClientSecret  = "fake-client-secret"
	fakeAdminPassword = "fake-admin-password"
	fakeCertificate   = "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----"
	fakeSessionCookie = "casdoor_session_id"
)

// fakeCasdoor serves the part of the Casdoor API the operator uses, from memory.
type fakeCasdoor struct {
	mu      sync.Mutex
	objects map[string]casdoor.Object
	server  *httptest.Server
}

func newFakeCasdoor() *fakeCasdoor {
	f := &fakeCasdoor{objects: map[string]casdoor.Object{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *fakeCasdoor) get(kind string, id string) casdoor.Object {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[kind+"/"+id]
	if !ok {
		return nil
	}
	copied := casdoor.Object{}
	for key, value := range obj {
		copied[key] = value
	}
	return copied
}

func (f *fakeCasdoor) put(kind string, obj casdoor.Object) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[kind+"/admin/"+obj["name"].(string)] = obj
}

func reply(w http.ResponseWriter, status string, msg string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": status, "msg": msg, "data": data})
}

func (f *fakeCasdoor) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")

	if path == "login" {
		body := map[string]string{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "admin" || body["password"] != fakeAdminPassword {
			reply(w, "error", "password or code is incorrect", nil)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: fakeSessionCookie, Value: "admin", Path: "/"})
		reply(w, "ok", "", "built-in/admin")
		return
	}

	clientId, clientSecret, basic := r.BasicAuth()
	cookie, _ := r.Cookie(fakeSessionCookie)
	if !(basic && clientId == fakeClientId && clientSecret == fakeClientSecret) && cookie == nil {
		reply(w, "error", "Unauthorized operation", nil)
		return
	}

	id := r.URL.Query().Get("id")
	switch {
	case path == "get-application" && id == casdoor.BuiltInApplicationId:
		reply(w, "ok", "", map[string]string{"clientId": fakeClientId, "clientSecret": fakeClientSecret})
	case path == "get-cert":
		reply(w, "ok", "", map[string]string{"name": strings.TrimPrefix(id, "admin/"), "certificate": fakeCertificate})
	case strings.HasPrefix(path, "get-"):
		reply(w, "ok", "", f.get(strings.TrimPrefix(path, "get-"), id))
	case strings.HasPrefix(path, "add-"):
		kind := strings.TrimPrefix(path, "add-")
		obj := casdoor.Object{}
		_ = json.NewDecoder(r.Body).Decode(&obj)
		if f.get(kind, "admin/"+obj["name"].(string)) != nil {
			reply(w, "ok", "", "Unaffected")
			return
		}
		if kind == casdoor.KindApplication {
			if obj["clientId"] == nil {
				obj["clientId"] = "generated-" + obj["name"].(string)
			}
			if obj["clientSecret"] == nil {
				obj["clientSecret"] = "generated-secret"
			}
		}
		f.put(kind, obj)
		reply(w, "ok", "", "Affected")
	case strings.HasPrefix(path, "update-"):
		obj := casdoor.Object{}
		_ = json.NewDecoder(r.Body).Decode(&obj)
		f.put(strings.TrimPrefix(path, "update-"), obj)
		reply(w, "ok", "", "Affected")
	case strings.HasPrefix(path, "delete-"):
		kind := strings.TrimPrefix(path, "delete-")
		obj := casdoor.Object{}
		_ = json.NewDecoder(r.Body).Decode(&obj)
		f.mu.Lock()
		delete(f.objects, kind+"/admin/"+obj["name"].(string))
		f.mu.Unlock()
		reply(w, "ok", "", "Affected")
	default:
		reply(w, "error", "unknown path "+path, nil)
	}
}

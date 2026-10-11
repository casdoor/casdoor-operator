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

// Package casdoor talks to the Casdoor API with objects kept as JSON maps, so that fields the
// operator does not know about survive a read-modify-write.
package casdoor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const (
	KindOrganization = "organization"
	KindApplication  = "application"

	BuiltInApplicationId = "admin/app-built-in"
)

type Object = map[string]interface{}

type Client struct {
	endpoint     string
	clientId     string
	clientSecret string
	http         *http.Client
}

type response struct {
	Status string          `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
}

func NewClient(endpoint string, clientId string, clientSecret string) *Client {
	return &Client{
		endpoint:     strings.TrimRight(endpoint, "/"),
		clientId:     clientId,
		clientSecret: clientSecret,
		http:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method string, path string, query url.Values, body interface{}) (json.RawMessage, error) {
	target := c.endpoint + path
	if len(query) != 0 {
		target += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.clientId != "" {
		req.SetBasicAuth(c.clientId, c.clientSecret)
	}
	return send(c.http, req)
}

func send(httpClient *http.Client, req *http.Request) (json.RawMessage, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s %s: HTTP %d: %s", req.Method, req.URL.Path, resp.StatusCode, truncate(payload))
	}

	res := response{}
	if err = json.Unmarshal(payload, &res); err != nil {
		return nil, fmt.Errorf("%s %s: unexpected response: %s", req.Method, req.URL.Path, truncate(payload))
	}
	if res.Status != "ok" {
		return nil, fmt.Errorf("%s %s: %s", req.Method, req.URL.Path, res.Msg)
	}
	return res.Data, nil
}

func truncate(payload []byte) string {
	if len(payload) > 200 {
		return string(payload[:200]) + "..."
	}
	return string(payload)
}

// GetObject returns nil when the object does not exist.
func (c *Client) GetObject(ctx context.Context, kind string, id string) (Object, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/get-"+kind, url.Values{"id": {id}}, nil)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}

	obj := Object{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err = decoder.Decode(&obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (c *Client) AddObject(ctx context.Context, kind string, obj Object) error {
	data, err := c.do(ctx, http.MethodPost, "/api/add-"+kind, nil, obj)
	if err != nil {
		return err
	}
	if string(data) == `"Unaffected"` {
		return fmt.Errorf("add %s %v: Casdoor did not create it", kind, obj["name"])
	}
	return nil
}

func (c *Client) UpdateObject(ctx context.Context, kind string, id string, obj Object) error {
	_, err := c.do(ctx, http.MethodPost, "/api/update-"+kind, url.Values{"id": {id}}, obj)
	return err
}

func (c *Client) DeleteObject(ctx context.Context, kind string, obj Object) error {
	_, err := c.do(ctx, http.MethodPost, "/api/delete-"+kind, nil, obj)
	return err
}

// GetCertificate returns the public certificate of a cert object.
func (c *Client) GetCertificate(ctx context.Context, id string) (string, error) {
	cert, err := c.GetObject(ctx, "cert", id)
	if err != nil || cert == nil {
		return "", err
	}
	certificate, _ := cert["certificate"].(string)
	return certificate, nil
}

// FetchBuiltInCredentials signs in as built-in/admin and reads the client credentials of
// app-built-in, which act as a global administrator.
func FetchBuiltInCredentials(ctx context.Context, endpoint string, password string) (string, string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", "", err
	}
	httpClient := &http.Client{Timeout: 30 * time.Second, Jar: jar}
	endpoint = strings.TrimRight(endpoint, "/")

	login, err := json.Marshal(map[string]string{
		"application":  "app-built-in",
		"organization": "built-in",
		"username":     "admin",
		"password":     password,
		"type":         "login",
		"signinMethod": "Password",
	})
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/login", bytes.NewReader(login))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if _, err = send(httpClient, req); err != nil {
		return "", "", fmt.Errorf("sign in as built-in/admin: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/get-application?id="+url.QueryEscape(BuiltInApplicationId), nil)
	if err != nil {
		return "", "", err
	}
	data, err := send(httpClient, req)
	if err != nil {
		return "", "", err
	}

	application := struct {
		ClientId     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}{}
	if err = json.Unmarshal(data, &application); err != nil {
		return "", "", err
	}
	if application.ClientId == "" || application.ClientSecret == "" || application.ClientSecret == "***" {
		return "", "", fmt.Errorf("could not read the credentials of %s", BuiltInApplicationId)
	}
	return application.ClientId, application.ClientSecret, nil
}

// Merge writes the desired fields over the current object and reports whether anything changed.
func Merge(current Object, desired Object) bool {
	changed := false
	for key, value := range desired {
		if !jsonEqual(current[key], value) {
			current[key] = value
			changed = true
		}
	}
	return changed
}

func jsonEqual(a interface{}, b interface{}) bool {
	return normalize(a) == normalize(b)
}

func normalize(value interface{}) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	var generic interface{}
	if err = json.Unmarshal(payload, &generic); err != nil {
		return string(payload)
	}
	payload, _ = json.Marshal(generic)
	return string(payload)
}

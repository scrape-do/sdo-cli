// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJobsCreateCommand(t *testing.T) {
	var method, path, token, contentType string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		token = r.Header.Get("X-Token")
		contentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"JobID":"job-1","Message":"queued","TaskIDs":["t1","t2"]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)

	out, errOut, err := execute(t, "jobs", "create", "https://a.com", "https://b.com",
		"--token", "t", "--option", "Super=true", "--option", "GeoCode=us")
	if err != nil {
		t.Fatalf("jobs create: %v", err)
	}
	if method != http.MethodPost || path != "/api/v1/jobs" {
		t.Errorf("request line: %s %s", method, path)
	}
	if token != "t" {
		t.Errorf("X-Token = %q, want t", token)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q", contentType)
	}
	targets, _ := body["Targets"].([]any)
	if len(targets) != 2 || targets[0] != "https://a.com" || targets[1] != "https://b.com" {
		t.Errorf("Targets = %v", body["Targets"])
	}
	// Super=true coerces to a JSON bool; GeoCode=us stays a string.
	if body["Super"] != true || body["GeoCode"] != "us" {
		t.Errorf("options not merged/coerced into body: %v", body)
	}
	if !strings.Contains(out, `"JobID": "job-1"`) {
		t.Errorf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "created job job-1") || !strings.Contains(errOut, "2 tasks") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestJobsStatusCommand(t *testing.T) {
	var method, path, token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		token = r.Header.Get("X-Token")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"JobID":"job-1","Status":"running","Tasks":[{"TaskID":"t1","URL":"https://a.com","Status":"done"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)

	out, _, err := execute(t, "jobs", "status", "job-1", "--token", "t")
	if err != nil {
		t.Fatalf("jobs status: %v", err)
	}
	if method != http.MethodGet || path != "/api/v1/jobs/job-1" {
		t.Errorf("request line: %s %s", method, path)
	}
	if token != "t" {
		t.Errorf("X-Token = %q", token)
	}
	if !strings.Contains(out, `"Status": "running"`) || !strings.Contains(out, `"TaskID": "t1"`) {
		t.Errorf("stdout = %q", out)
	}
}

func TestJobsResultCommand(t *testing.T) {
	var method, path, token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		token = r.Header.Get("X-Token")
		w.Header().Set("Content-Type", "application/json")
		// Base64EncodedContent is a flag: Content is base64 and decodes to "hello".
		w.Write([]byte(`{"TaskID":"t1","JobID":"job-1","URL":"https://a.com","Status":"success","StatusCode":200,"Base64EncodedContent":true,"Content":"aGVsbG8="}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)

	out, errOut, err := execute(t, "jobs", "result", "job-1", "t1", "--token", "t")
	if err != nil {
		t.Fatalf("jobs result: %v", err)
	}
	if method != http.MethodGet || path != "/api/v1/jobs/job-1/t1" {
		t.Errorf("request line: %s %s", method, path)
	}
	if token != "t" {
		t.Errorf("X-Token = %q", token)
	}
	if out != "hello" {
		t.Errorf("stdout = %q, want decoded base64 content", out)
	}
	if !strings.Contains(errOut, "status=200") || !strings.Contains(errOut, "task=t1") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestJobsListCommand(t *testing.T) {
	var method, path, token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		token = r.Header.Get("X-Token")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Jobs":[{"JobID":"job-1","Status":"done"},{"JobID":"job-2","Status":"running"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)

	out, _, err := execute(t, "jobs", "list", "--token", "t")
	if err != nil {
		t.Fatalf("jobs list: %v", err)
	}
	if method != http.MethodGet || path != "/api/v1/jobs" {
		t.Errorf("request line: %s %s", method, path)
	}
	if token != "t" {
		t.Errorf("X-Token = %q", token)
	}
	if !strings.Contains(out, `"JobID": "job-1"`) || !strings.Contains(out, `"JobID": "job-2"`) {
		t.Errorf("stdout = %q", out)
	}
}

func TestJobsCancelCommand(t *testing.T) {
	var method, path, token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		token = r.Header.Get("X-Token")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)

	_, errOut, err := execute(t, "jobs", "cancel", "job-1", "--token", "t")
	if err != nil {
		t.Fatalf("jobs cancel: %v", err)
	}
	if method != http.MethodDelete || path != "/api/v1/jobs/job-1" {
		t.Errorf("request line: %s %s", method, path)
	}
	if token != "t" {
		t.Errorf("X-Token = %q", token)
	}
	if !strings.Contains(errOut, "canceled job job-1") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestAccountCommand(t *testing.T) {
	var method, path, token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		token = r.Header.Get("X-Token")
		w.Header().Set("Content-Type", "application/json")
		// API misspells the wire key "AvaliableCredits".
		w.Write([]byte(`{"TotalConcurrency":10,"FreeConcurrency":4,"ActiveJobs":2,"AvaliableCredits":500}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)

	out, _, err := execute(t, "account", "--token", "t")
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	if method != http.MethodGet || path != "/api/v1/me" {
		t.Errorf("request line: %s %s", method, path)
	}
	if token != "t" {
		t.Errorf("X-Token = %q", token)
	}
	// The CLI must expose the corrected key and never leak the API's typo.
	if !strings.Contains(out, `"AvailableCredits": 500`) {
		t.Errorf("stdout = %q, want corrected AvailableCredits key", out)
	}
	if strings.Contains(out, "Avaliable") {
		t.Errorf("stdout leaks the API's misspelled key: %q", out)
	}
}

func TestJobsErrorEnvelopeCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"Error":"bad targets","Code":400}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)

	_, _, err := execute(t, "jobs", "create", "https://a.com", "--token", "t")
	if err == nil {
		t.Fatal("expected error from 400 response")
	}
	if !strings.Contains(err.Error(), "bad targets") {
		t.Errorf("error = %v, want parsed envelope message", err)
	}
}

func TestParseOptions(t *testing.T) {
	opts, err := parseOptions([]string{"Super=true", "GeoCode=us", "Count=3"})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if opts["Super"] != true {
		t.Errorf("Super = %#v, want bool true", opts["Super"])
	}
	if opts["GeoCode"] != "us" {
		t.Errorf("GeoCode = %#v, want string us", opts["GeoCode"])
	}
	if opts["Count"] != json.Number("3") {
		t.Errorf("Count = %#v, want number 3", opts["Count"])
	}
	if _, err := parseOptions([]string{"bad"}); err == nil {
		t.Fatal("expected error for malformed option")
	}
}

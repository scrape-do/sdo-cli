// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// `sdo jobs result` exits 0 only for a successful task, so a script's
// `> page.html` cannot take an empty file for a page. A task that has not
// finished exits 4 (poll again); a failed, canceled or unknown one exits 1.
// Nothing reaches stdout then, except the JSON with --json.
func TestJobsResultExitCodeFollowsTheTaskState(t *testing.T) {
	var state, message string
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		b, _ := json.Marshal(map[string]any{"TaskID": "t1", "Status": state, "ErrorMessage": message, "Content": "partial"})
		w.Write(b)
	})
	cases := []struct {
		state, message string
		code           int
		want           string
	}{
		{"success", "", exitOK, ""},
		{"SUCCESS", "", exitOK, ""},
		{"error", "Target timed out after 3 retries", exitFailure, "task t1 did not succeed (state=error): Target timed out after 3 retries"},
		{"canceled", "", exitFailure, "task t1 did not succeed (state=canceled)"},
		{"pending", "", exitPending, "task t1 has not finished yet (state=pending); try again later"},
		{"queued", "", exitPending, "state=queued"},
		{"queuing", "", exitPending, "state=queuing"},
		{"rotating", "", exitPending, "state=rotating"},
		{"", "", exitFailure, `unknown state ""`},
		{"exploded", "", exitFailure, `unknown state "exploded"`},
	}
	for _, c := range cases {
		state, message = c.state, c.message
		out, errOut, code := runCLI(t, "", "--token", "t", "jobs", "result", "j1", "t1")
		if code != c.code || !strings.Contains(errOut, c.want) {
			t.Errorf("state %q: exit %d, stderr %q, want %d and %q", c.state, code, errOut, c.code, c.want)
		}
		if wantOut := map[bool]string{true: "partial", false: ""}[c.code == exitOK]; out != wantOut {
			t.Errorf("state %q: stdout %q, want %q", c.state, out, wantOut)
		}
		out, _, code = runCLI(t, "", "--token", "t", "jobs", "result", "j1", "t1", "--json")
		if code != c.code || !json.Valid([]byte(out)) {
			t.Errorf("state %q --json: exit %d, stdout %q", c.state, code, out)
		}
	}
}

// The API misspells the credits key; either spelling reaches the output under
// the corrected name.
func TestAccountReadsEitherCreditsSpelling(t *testing.T) {
	var body string
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	})
	for _, body = range []string{`{"AvaliableCredits":1000}`, `{"AvailableCredits":1000}`} {
		out, errOut, code := runCLI(t, "", "--token", "t", "account")
		if code != 0 || !strings.Contains(out, `"AvailableCredits": 1000`) || strings.Contains(out, "Avaliable") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", body, code, out, errOut)
		}
	}
}

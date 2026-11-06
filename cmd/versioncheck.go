// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/scrape-do/sdo-cli/internal/version"
)

// `sdo version --check` is the CLI's only request to anything but Scrape.do,
// and it runs only when asked: the no-telemetry promise (SECURITY.md) allows
// this one on-demand question and nothing automatic.

// defaultGitHubAPI is where the check asks. It is a package variable rather
// than an environment variable so tests can point it at their own server
// while a user cannot redirect the check by accident.
const defaultGitHubAPI = "https://api.github.com"

var (
	githubAPI           = defaultGitHubAPI
	versionCheckTimeout = 5 * time.Second
)

// maxReleaseBody bounds what the check reads. A release object is a few KiB;
// anything larger is not the answer we asked for.
const maxReleaseBody = 64 << 10

// releaseTag is the shape our tag workflow enforces for a final release. The
// API's "latest" never names a pre-release, so a tag of any other shape means
// the answer cannot be trusted and is not echoed.
var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

var (
	errRedirect      = errors.New("GitHub answered with a redirect, which is not followed")
	errNotARelease   = errors.New("the response is not a release")
	errUnknownLatest = errors.New("the latest release could not be determined")
)

// latestRelease asks GitHub for the newest final release of sdo-cli. It sends
// no credential of any kind: the Scrape.do token is never read on this path.
// The errors it returns never contain text from the response.
func latestRelease(ctx context.Context) (semver, error) {
	client := &http.Client{
		Timeout: versionCheckTimeout,
		// A redirect could take the request to another host; refuse it.
		CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		githubAPI+"/repos/scrape-do/sdo-cli/releases/latest", nil)
	if err != nil {
		return semver{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", version.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return semver{}, transportReason("the request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("GitHub answered HTTP %d", resp.StatusCode)
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			msg += " (its limit for anonymous requests may be used up; try again later)"
		}
		return semver{}, errors.New(msg)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseBody+1))
	if err != nil {
		return semver{}, transportReason("reading the response failed", err)
	}
	if len(body) > maxReleaseBody {
		return semver{}, errors.New("the response is larger than 64 KiB")
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if json.Unmarshal(body, &release) != nil {
		return semver{}, errNotARelease
	}
	if !releaseTag.MatchString(release.TagName) {
		return semver{}, errUnknownLatest
	}
	v, ok := parseVersion(release.TagName[1:])
	if !ok { // leading zeros, or a number too large
		return semver{}, errUnknownLatest
	}
	return v, nil
}

// transportReason keeps a network failure short: the cause without the URL.
func transportReason(what string, err error) error {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return fmt.Errorf("no answer from GitHub within %s", versionCheckTimeout)
	}
	if errors.Is(err, errRedirect) {
		return errRedirect
	}
	var ue interface{ Unwrap() error }
	if errors.As(err, &ue) && ue.Unwrap() != nil {
		err = ue.Unwrap()
	}
	return fmt.Errorf("%s: %w", what, err)
}

// reportRelease compares the installed version with the latest release. A
// newer release is news, not a failure, so it never returns an error for it.
func reportRelease(w io.Writer, installed string, latest semver) error {
	cur, ok := parseVersion(installed)
	if !ok {
		return fmt.Errorf("the installed version %q is not a release version", installed)
	}
	tag := "v" + latest.String()
	var msg string
	switch c := compareVersions(cur, latest); {
	case c < 0:
		msg = fmt.Sprintf("latest release: %s (installed: %s)\n", tag, installed) +
			"upgrade: brew upgrade sdo\n" +
			"upgrade: scoop update sdo\n" +
			"upgrade: go install github.com/scrape-do/sdo-cli/cmd/sdo@latest\n" +
			"download: https://github.com/scrape-do/sdo-cli/releases/tag/" + tag + "\n"
	case c == 0:
		msg = fmt.Sprintf("latest release: %s (up to date)\n", tag)
	default:
		msg = fmt.Sprintf("latest release: %s (installed %s is newer)\n", tag, installed)
	}
	_, err := io.WriteString(w, msg)
	return err
}

// semver is a SemVer 2.0.0 version without build metadata, which no sdo
// version carries.
type semver struct {
	major, minor, patch uint64
	pre                 []string // pre-release identifiers; nil for a final release
}

func (v semver) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if v.pre != nil {
		s += "-" + strings.Join(v.pre, ".")
	}
	return s
}

// parseVersion accepts exactly MAJOR.MINOR.PATCH[-PRERELEASE] as SemVer
// defines them: no "v", no leading zeros, no build metadata.
func parseVersion(s string) (semver, bool) {
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var nums [3]uint64
	for i, p := range parts {
		n, ok := numericID(p)
		if !ok {
			return semver{}, false
		}
		nums[i] = n
	}
	v := semver{major: nums[0], minor: nums[1], patch: nums[2]}
	if !hasPre {
		return v, true
	}
	v.pre = strings.Split(pre, ".")
	for _, id := range v.pre {
		if !validPreID(id) {
			return semver{}, false
		}
	}
	return v, true
}

// numericID parses a numeric identifier: digits only, no leading zero.
func numericID(s string) (uint64, bool) {
	if s == "" || (len(s) > 1 && s[0] == '0') || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

func validPreID(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '-') {
			return false
		}
	}
	if strings.Trim(id, "0123456789") == "" { // all digits: a numeric identifier
		_, ok := numericID(id)
		return ok
	}
	return true
}

// compareVersions orders by SemVer precedence: 0.10.0 after 0.9.0, and a
// pre-release before its final version.
func compareVersions(a, b semver) int {
	for _, d := range [][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if d[0] != d[1] {
			return cmpUint(d[0], d[1])
		}
	}
	switch {
	case a.pre == nil && b.pre == nil:
		return 0
	case a.pre == nil:
		return 1
	case b.pre == nil:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePreID(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmpUint(uint64(len(a.pre)), uint64(len(b.pre)))
}

// comparePreID compares two valid identifiers: numeric ones numerically and
// below alphanumeric ones, which compare in ASCII order.
func comparePreID(a, b string) int {
	an, aNum := numericID(a)
	bn, bNum := numericID(b)
	switch {
	case aNum && bNum:
		return cmpUint(an, bn)
	case aNum:
		return -1
	case bNum:
		return 1
	}
	return strings.Compare(a, b)
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

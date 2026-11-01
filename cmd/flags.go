// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"time"
)

// secondsRe is SDO_TIMEOUT's grammar, which --client-timeout shares: a plain
// decimal number of seconds (no sign, exponent, hex or underscores).
var secondsRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// secondsValue is a pflag.Value for a client timeout in seconds, at least
// 0.001. Huge values are clamped to the longest time.Duration.
type secondsValue struct{ d *time.Duration }

func (v *secondsValue) Set(s string) error {
	secs, _ := strconv.ParseFloat(s, 64) // the grammar leaves only ErrRange (+Inf, clamped below)
	if !secondsRe.MatchString(s) || secs < 0.001 {
		return errors.New("want a number of seconds, at least 0.001 (e.g. 90 or 2.5)")
	}
	if secs >= float64(math.MaxInt64)/float64(time.Second) {
		*v.d = time.Duration(math.MaxInt64)
	} else {
		*v.d = time.Duration(secs * float64(time.Second))
	}
	return nil
}

func (v *secondsValue) String() string {
	if *v.d == 0 {
		return ""
	}
	return strconv.FormatFloat(v.d.Seconds(), 'f', -1, 64)
}

func (v *secondsValue) Type() string { return "seconds" }

// bytesValue is a pflag.Value for a positive byte count.
type bytesValue struct{ n *int64 }

func (v *bytesValue) Set(s string) error {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return errors.New("want a positive whole number of bytes (e.g. 67108864 for 64 MiB)")
	}
	*v.n = n
	return nil
}

func (v *bytesValue) String() string {
	if *v.n == 0 {
		return ""
	}
	return strconv.FormatInt(*v.n, 10)
}

func (v *bytesValue) Type() string { return "bytes" }

// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package condition provides simple text checking tools.
package condition

import (
	"fmt"
	"regexp"
	"strings"
)

// Condition is a cunjunction of tests against a string. Note that Contains and
// Regexp conditions both use the same Count; most likely one would use either
// Contains or Regexp but not both.
type Condition struct {
	Prefix string // Prefix is the required prefix
	Suffix string // Suffix is the required suffix.

	Contains string // Contains must be contained in the string.

	// Regexp is a regular expression to look for.
	Regexp *regexp.Regexp

	// Count determines how many oocurences of Contains or Regexp
	// are required for a match:
	//    0: Any positive number of matches is okay
	//   >0: Exactly that many matches required
	//   <0: No match allowed (invert the condition)
	Count int

	// Min and Max are the minimum and maximum length the string may
	// have. Two zero values disables this test.
	Min, Max int
}

// Fullfilled returns whether s matches all requirements of c.
// A nil return value indicates that s matches the defined conditions.
// A non-nil return indicates missmatch.
func (c Condition) Fullfilled(s string) error {
	if c.Prefix != "" && !strings.HasPrefix(s, c.Prefix) {
		n := len(c.Prefix)
		if len(s) < n {
			n = len(s)
		}
		return fmt.Errorf("Bad prefix, got %q", s[:n])
	}

	if c.Suffix != "" && !strings.HasSuffix(s, c.Suffix) {
		n := len(c.Suffix)
		if len(s) < n {
			n = len(s)
		}
		return fmt.Errorf("Bad suffix, got %q", s[len(s)-n:])
	}

	if c.Contains != "" {
		if c.Count == 0 && strings.Index(s, c.Contains) == -1 {
			return fmt.Errorf("Missing text")
		} else if c.Count < 0 && strings.Index(s, c.Contains) != -1 {
			return fmt.Errorf("Forbidden text")
		} else if c.Count > 0 {
			if cnt := strings.Count(s, c.Contains); cnt != c.Count {
				return fmt.Errorf("Found %d occurences", cnt)
			}
		}
	}

	if c.Regexp != nil {
		if c.Count == 0 && c.Regexp.FindStringIndex(s) == nil {
			return fmt.Errorf("Missing match")
		} else if c.Count < 0 && c.Regexp.FindStringIndex(s) != nil {
			return fmt.Errorf("Forbidden match")
		} else if c.Count > 0 {
			if m := c.Regexp.FindAllString(s, -1); len(m) != c.Count {
				return fmt.Errorf("Found %d matches", len(m))
			}
		}
	}

	if c.Min > 0 {
		if len(s) < c.Min {
			return fmt.Errorf("Too short, was %d", len(s))
		}
	}

	if c.Max > 0 {
		if len(s) > c.Max {
			return fmt.Errorf("Too long, was %d", len(s))
		}
	}

	return nil
}
// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ht

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Repeat returns count copies of test with variables replaced based
// on vars. The keys of vars are the variable names. The values of a
// variable v are choosen from vars[v] by cycling through the list:
// In the n'th repetition is vars[v][n%N] with N=len(vars[v])).
func Repeat(test *Test, count int, vars map[string][]string) ([]*Test, error) {
	reps := make([]*Test, count)
	for r := 0; r < count; r++ {
		curVars := make(map[string]string)
		for k, v := range vars {
			curVars[k] = v[r%len(v)]
		}
		replacer, err := newReplacer(curVars)
		if err != nil {
			return nil, err
		}

		reps[r] = test.substituteVariables(replacer)
		for k, v := range curVars {
			reps[r].Description += fmt.Sprintf("\nVar %s=%q", k, v)
		}
	}
	return reps, nil
}

// lcm computest the least common multiple of m and n.
func lcm(m, n int) int {
	a, b := m, n
	for a != b {
		if a < b {
			a += m
		} else {
			b += n
		}
	}
	return a
}

// lcmOf computes the least common multiple of the length of all valuesin vars.
func lcmOf(vars map[string][]string) int {
	n := 0
	for _, v := range vars {
		if n == 0 {
			n = len(v)
		} else {
			n = lcm(n, len(v))
		}
	}
	return n
}

// substituteVariables returns a copy of t with replacer applied.
func (t *Test) substituteVariables(repl replacer) *Test {
	// Apply to name, description, URL and body.
	c := &Test{
		Name:        repl.str.Replace(t.Name),
		Description: repl.str.Replace(t.Description),
		Request: Request{
			Method:          repl.str.Replace(t.Request.Method),
			URL:             repl.str.Replace(t.Request.URL),
			ParamsAs:        repl.str.Replace(t.Request.ParamsAs),
			Body:            repl.str.Replace(t.Request.Body),
			FollowRedirects: t.Request.FollowRedirects,
		},
		Poll:        t.Poll,
		Timeout:     t.Timeout,
		Verbosity:   t.Verbosity,
		PreSleep:    t.PreSleep,
		InterSleep:  t.InterSleep,
		PostSleep:   t.PostSleep,
		ClientPool:  t.ClientPool,
		VarEx:       t.VarEx,
		Criticality: t.Criticality,
	}

	// Apply to request parameters.
	c.Request.Params = make(URLValues)
	for param, vals := range t.Request.Params {
		rv := make([]string, len(vals))
		for i, v := range vals {
			rv[i] = repl.str.Replace(v)
		}
		c.Request.Params[param] = rv
	}

	// Apply to http header.
	c.Request.Header = make(http.Header)
	for h, vals := range t.Request.Header {
		rv := make([]string, len(vals))
		for i, v := range vals {
			rv[i] = repl.str.Replace(v)
		}
		c.Request.Header[h] = rv
	}

	// Apply to cookie values.
	for _, cookie := range t.Request.Cookies {
		rc := Cookie{Name: cookie.Name, Value: repl.str.Replace(cookie.Value)}
		c.Request.Cookies = append(c.Request.Cookies, rc)
	}

	// Apply to checks.
	c.Checks = make([]Check, len(t.Checks))
	for i := range t.Checks {
		c.Checks[i] = SubstituteVariables(t.Checks[i], repl.str, repl.fn)
	}

	return c
}

// ----------------------------------------------------------------------------
// Variable substitutions

var nowTimeRe = regexp.MustCompile(`{{NOW *([+-] *[1-9][0-9]*[smhd])? *(\| *"(.*)")?}}`)

// addSpecialVariables adds all special variables of the forms
//     {{NOW ...}}  and
//     {{RANDOM ...}}
// in s to the map m.
// TODO: replace regexp matching with fasterand simpler code.
func addSpecialVariables(s string, m map[string]struct{}) {
	for _, match := range nowTimeRe.FindAllString(s, -1) {
		m[match] = struct{}{}
	}
	for _, match := range randomRe.FindAllString(s, -1) {
		m[match] = struct{}{}
	}
}

// findSpecialVariables return all occurences of special variables
// as defined in addSpecialVariables
func (t *Test) findSpecialVariables() map[string]struct{} {
	v := map[string]struct{}{}

	addSpecialVariables(t.Name, v)
	addSpecialVariables(t.Description, v)
	addSpecialVariables(t.Request.URL, v)
	addSpecialVariables(t.Request.Body, v)
	for _, pp := range t.Request.Params {
		for _, p := range pp {
			addSpecialVariables(p, v)
		}
	}
	for _, hh := range t.Request.Header {
		for _, h := range hh {
			addSpecialVariables(h, v)
		}
	}
	for _, cookie := range t.Request.Cookies {
		addSpecialVariables(cookie.Value, v)
	}
	for _, c := range t.Checks {
		findSpecialVarsInCheck(c, v)
	}
	return v
}

func findSpecialVarsInCheck(check Check, m map[string]struct{}) {
	v := reflect.ValueOf(check)
	findSpecialVarsInValue(v, m)
}

func findSpecialVarsInValue(v reflect.Value, m map[string]struct{}) {
	switch v.Kind() {
	case reflect.String:
		addSpecialVariables(v.String(), m)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			findSpecialVarsInValue(v.Field(i), m)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			findSpecialVarsInValue(v.Index(i), m)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			findSpecialVarsInValue(v.MapIndex(key), m)
		}
	case reflect.Ptr:
		v = v.Elem()
		if !v.IsValid() {
			return
		}
		findSpecialVarsInValue(v, m)
	case reflect.Interface:
		v = v.Elem()
		findSpecialVarsInValue(v, m)
	}
}

// specialVariables produces values for all names of special variables.
func specialVariables(now time.Time, names map[string]struct{}) (map[string]string, error) {
	vars := make(map[string]string)
	for k, _ := range names {
		if strings.HasPrefix(k, "{{NOW") {
			err := setNowVariable(vars, now, k)
			if err != nil {
				return vars, err
			}
		} else {
			// Must be "{{RANDOM".
			err := setRandomVariable(vars, k)
			if err != nil {
				return vars, err
			}
		}
	}
	return vars, nil
}

// interprete k of the form {{NOW ...}} and set vars[k] to that vlaue.
func setNowVariable(vars map[string]string, now time.Time, k string) error {
	m := nowTimeRe.FindAllStringSubmatch(k, 1)
	if m == nil {
		panic("Unmatchable " + k)
	}
	kk := k[2 : len(k)-2] // Remove {{ and }} to produce the "variable name".
	if _, ok := vars[kk]; ok {
		return nil // We already processed this variable.
	}
	var off time.Duration
	delta := m[0][1]
	if delta != "" {
		num := strings.TrimLeft(delta[1:len(delta)-1], " ")
		n, err := strconv.Atoi(num)
		if err != nil {
			return err
		}
		if delta[0] == '-' {
			n *= -1
		}
		switch delta[len(delta)-1] {
		case 's':
			n *= 1
		case 'm':
			n *= 60
		case 'h':
			n *= 60 * 60
		case 'd':
			n *= 24 * 26 * 60
		default:
			return fmt.Errorf("ht: bad now-variable delta unit %q", delta[len(delta)-1])
		}
		off = time.Duration(n) * time.Second
	}
	format := time.RFC1123
	if m[0][3] != "" {
		format = m[0][3]
	}
	formatedTime := now.Add(off).Format(format)
	vars[kk] = formatedTime
	return nil
}

// mergeVariables merges all variables found in the various vars.
func mergeVariables(vars ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, e := range vars {
		for k, v := range e {
			result[k] = v
		}
	}
	return result
}

// replacer determines a set of string and integer replacements
// for the variable substitutions.
type replacer struct {
	str *strings.Replacer
	fn  map[int64]int64
}

// newReplacer produces a replacer to perform substitution of the
// given variables with their values. A key of the form "#123" (i.e. hash
// followed by literal decimal integer) is treated as an integer substitution.
// Other keys are treated as string variables which subsitutes "{{k}}" with
// vars[k] for a key k. Maybe just have a look at the code.
func newReplacer(vars map[string]string) (replacer, error) {
	oldnew := []string{}
	fn := make(map[int64]int64)
	for k, v := range vars {
		if strings.HasPrefix(k, "#") {
			// An integer substitution
			o, err := strconv.ParseInt(k[1:], 10, 64)
			if err != nil {
				return replacer{}, err
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return replacer{}, err
			}
			fn[o] = n
		} else {
			// A string substitution.
			oldnew = append(oldnew, "{{"+k+"}}") // TODO: make configurable ??
			oldnew = append(oldnew, v)
		}
	}

	return replacer{
		str: strings.NewReplacer(oldnew...),
		fn:  fn,
	}, nil
}

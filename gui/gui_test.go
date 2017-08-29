// Copyright 2017 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gui

import (
	"bytes"
	"flag"
	"log"
	"net/http"
	"reflect"
	"regexp"
	"testing"
	"time"
)

// ----------------------------------------------------------------------------
// An interface and two implementations.

type Writer interface {
	Write()
}
type AbcWriter struct{ Name string }

func (AbcWriter) Write() {}

type XyzWriter struct{ Count int }

func (XyzWriter) Write() {}

type PtrWriter struct {
	Full     bool
	Fraction float64
}

func (*PtrWriter) Write() {}

// ----------------------------------------------------------------------------
// Test is a demo structure

type Test struct {
	Name        string
	Description string
	Primes      []int
	Output      string
	Chan        chan string
	Func        func(int) bool
	Options     Options
	Execution   Execution
	Result      Result
	Pointer     *int
	NilPointer  *int
	Fancy       map[string][]string
	Writer      Writer
	Writers     []Writer
}

type Result struct {
	Okay     bool
	Count    int
	State    string
	Frac     float64
	Details  []string
	Options  *Options
	Vars     map[string]string
	Duration time.Duration
	Time     time.Time
}

type Options struct {
	Simple   string
	Advanced time.Duration
	Complex  struct {
		Foo string
		Bar string
	}
	Started time.Time
}

type Execution struct {
	Method     string
	Tries      int
	Wait       time.Duration
	Hash       string
	Env        map[string]string
	unexported int
}

func registerTestTypes() {
	RegisterType(Test{}, Typeinfo{
		Doc: "Test to perform.",
		Field: map[string]Fieldinfo{
			"Name":        Fieldinfo{Doc: "Must be unique."},
			"Description": Fieldinfo{Multiline: true},
			"Output":      Fieldinfo{Const: true},
			"Result":      Fieldinfo{Const: true},
		},
	})

	RegisterType(Execution{}, Typeinfo{
		Doc: "Controles test execution",
		Field: map[string]Fieldinfo{
			"Method": Fieldinfo{
				Doc:  "The HTTP method",
				Only: []string{"GET", "POST", "HEAD"},
			},
			"Tries": Fieldinfo{
				Doc: "Number of retries.",
			},
			"Wait": Fieldinfo{
				Doc: "Sleep duration between retries.",
			},
			"Hash": Fieldinfo{
				Doc:      "Hash in hex",
				Validate: regexp.MustCompile("^[[:xdigit:]]{4,8}$"),
			},
		},
	})

	RegisterType(Options{}, Typeinfo{
		Doc: "Options captures basic settings\nfor this piece of code.",
		Field: map[string]Fieldinfo{
			"Simple":   Fieldinfo{Doc: "Simple is the common name."},
			"Advanced": Fieldinfo{Doc: "Advanced contains admin options."},
			"Complex":  Fieldinfo{Doc: "Complex allows fancy customisations."},
		},
	})

	RegisterType(Result{}, Typeinfo{
		Doc: "Result of the Test",
	})

	RegisterType(AbcWriter{}, Typeinfo{
		Doc: "AbcWriter is for latin scripts",
		Field: map[string]Fieldinfo{
			"Name": Fieldinfo{Doc: "How to address this Writer."},
		},
	})

	RegisterType(XyzWriter{}, Typeinfo{
		Doc: "XyZWriter is a useless\ndummy type for testing",
		Field: map[string]Fieldinfo{
			"Count": Fieldinfo{Doc: "Ignored."},
		},
	})

	RegisterType(PtrWriter{}, Typeinfo{
		Doc: "PtrWriter: Only *PtrWriter are Writers",
		Field: map[string]Fieldinfo{
			"Full":     Fieldinfo{Doc: "Full check"},
			"Fraction": Fieldinfo{Doc: "0 <= Fraction <=1"},
		},
	})

}

var test Test
var testgui = flag.Bool("gui", false, "actually serve a GUI under :8888")
var globalRenderer Value

func TestGUI(t *testing.T) {
	if !*testgui {
		t.Skip("Can be executed via cmdline flag -gui")
	}

	// Register types and implementations.
	Typedata = make(map[reflect.Type]Typeinfo)
	registerTestTypes()

	Implements = make(map[reflect.Type][]reflect.Type)
	RegisterImplementation((*Writer)(nil), AbcWriter{})
	RegisterImplementation((*Writer)(nil), XyzWriter{})
	RegisterImplementation((*Writer)(nil), &PtrWriter{})

	// Fill initial values of a Test.
	test = Test{}
	test.Name = "Hello World"
	test.Output = "Outcome of the Test is 'good'.\nNext run maybe too..."
	test.Primes = []int{2, 3, 5}
	test.Execution.Tries = 3
	test.Execution.unexported = -99
	test.Options.Advanced = 150 * time.Millisecond
	test.Options.Started = time.Now()
	test.Execution.Env = map[string]string{
		"Hello": "World",
		"ABC":   "XYZ",
	}
	var x int = 17
	test.Pointer = &x
	test.Fancy = map[string][]string{
		"Hund":  []string{"doof", "dreckig"},
		"katze": []string{"schlau"},
	}
	test.Writer = AbcWriter{"Heinz"}
	test.Writers = []Writer{XyzWriter{8}, AbcWriter{"Anna"}, &PtrWriter{}}
	test.Result.Okay = true
	test.Result.Count = 137
	test.Result.State = "Passed"
	test.Result.Frac = 0.75
	test.Result.Details = []string{"Executed", "Worked\nas intended", "<<super>>"}
	test.Result.Options = nil
	test.Result.Vars = map[string]string{"DE": "Deutsch", "FR": "Français"}
	test.Result.Duration = 137 * time.Millisecond
	test.Result.Time = time.Now()

	value := NewValue(test, "Test")

	http.HandleFunc("/favicon.ico", faviconHandler)
	http.HandleFunc("/display", displayHandler(value))
	http.HandleFunc("/update", updateHandler(value))
	log.Fatal(http.ListenAndServe(":8888", nil))
}

func displayHandler(val *Value) func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		buf := &bytes.Buffer{}
		writePreamble(buf, "Test")
		data, err := val.Render()
		buf.Write(data)
		writeEpilogue(buf)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		w.Write(buf.Bytes())

		if err != nil {
			log.Fatal(err)
		}
	}
}

func updateHandler(val *Value) func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		req.ParseForm()
		_, errlist := val.Update(req.Form)

		if len(errlist) == 0 {
			w.Header().Set("Location", "/display")
			w.WriteHeader(303)
			return
		}

		buf := &bytes.Buffer{}
		writePreamble(buf, "Bad input")
		data, _ := val.Render()
		buf.Write(data)
		writeEpilogue(buf)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(400)
		w.Write(buf.Bytes())
	}
}

func writePreamble(buf *bytes.Buffer, title string) {
	buf.WriteString(`<!doctype html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Check Builder</title>
    <style>
 `)
	buf.WriteString(CSS)
	buf.WriteString(`
    </style>
</head>
<body>
  <h1>` + title + `</h1>
  <form action="/update" method="post">
`)
}

func writeEpilogue(buf *bytes.Buffer) {
	buf.WriteString(`
    <div style="position: fixed; top:2%; right:2%;">
      </p>
        <button class="actionbutton" name="action" value="execute" style="background-color: #DDA0DD;"> Execute Test </button>
      </p>
      <p>
        <button class="actionbutton" name="action" value="runchecks" style="background-color: #FF8C00;"> Try Checks </button>
      <p>
        <button class="actionbutton" name="action" value="update"> Update Values </button>
      </p>
    </div>
  </form>
</body>
</html>
`)

}

func faviconHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "max-age=3600")
	w.Write(Favicon)
}
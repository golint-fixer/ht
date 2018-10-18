// Copyright 2016 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ht

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ----------------------------------------------------------------------------
// file:// pseudo-request

type fileAction struct{}

func (f fileAction) Schema() string { return "file" }

func (f fileAction) Valid(t *Test) error {
	switch t.Request.Method {
	case http.MethodGet, http.MethodPut, http.MethodDelete:
		return nil
	}
	return fmt.Errorf("method %s not supported on file:// URL", t.Request.Method)
}

// Execute a file:// pseudorequest. This method returns a non-nil error if
// the Request.Method is non of GET, PUT or DELETE. The file operations
// themself do not return an error but a status codes 404 or 403.
// This behaviour is in the line of how a HTTP request works and allows e.g.
// to check that a lock file is _not_ present.
//
// Once remote file operations via ssh are implemented a failer to connect
// to the remote host can be returned as an error. Again in accorancde
func (f fileAction) Execute(t *Test) error {
	t.infof("%s %q", t.Request.Request.Method, t.Request.Request.URL.String())

	start := time.Now()
	defer func() {
		t.Response.Duration = time.Since(start)
	}()

	u := t.Request.Request.URL
	if u.Host != "" {
		if u.Host != "localhost" && u.Host != "127.0.0.1" { // TODO IPv6
			return fmt.Errorf("file:// on remote host not implemented")
		}
	}

	// Fake a http.Response
	t.Response.Response = &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       nil, // already close and consumed
		Trailer:    make(http.Header),
		Request:    t.Request.Request,
	}

	switch t.Request.Method {
	case http.MethodGet:
		executeFileGET(t, u)
	case http.MethodPut:
		executeFilePUT(t, u)
	case http.MethodDelete:
		executeFileDELETE(t, u)
	default:
		panic("cannot happen")
	}

	return nil
}

func isWindowsDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func localFilename(p string) string {
	if runtime.GOOS == "windows" {
		// file: URLs on Windows have the form file:///D:/some/path
		// so u.Path == "/D:/some/path". Unfortunately this leading /
		// results in problems if u.Path is used as is.
		// The following code does not handle the //host/share/path
		// version of a file path. Oh how much I hate Windows.
		if len(p) > 3 && p[0] == '/' && isWindowsDriveLetter(p[1]) && p[2] == ':' {
			return p[1:]
		}
	}
	return p
}

//
//  Successfully wrote /home/volker/code/src/github.com/vdobler/ht/ht/testdata/fileprotocol
//  Successfully wrote
//

// file could be opened      --> 200
// any problems opening file --> 404
func executeFileGET(t *Test, u *url.URL) {
	filename := localFilename(u.Path)
	file, err := os.Open(filename)
	if err != nil {
		t.Response.Response.Status = "404 Not Found"
		t.Response.Response.StatusCode = 404
		t.Response.BodyStr = err.Error()
		return
	}
	defer file.Close()
	body, err := ioutil.ReadAll(file)
	t.Response.BodyStr = string(body)
	t.Response.BodyErr = err
}

// properly created --> 200
// any problems     --> 403
func executeFilePUT(t *Test, u *url.URL) {
	filename := localFilename(u.Path)
	err := ioutil.WriteFile(filename, []byte(t.Request.Body), 0666)
	if err != nil {
		t.Response.Response.Status = "403 Forbidden"
		t.Response.Response.StatusCode = 403
		t.Response.BodyStr = err.Error()
		return
	}
	t.Response.Response.Status = "200 OK"
	t.Response.Response.StatusCode = 200
	t.Response.BodyStr = fmt.Sprintf("Successfully wrote %s", u)
	t.Response.BodyErr = nil
}

// properly deleted     --> 200
// filename nonexisting --> 404
// unable to delete     --> 403
func executeFileDELETE(t *Test, u *url.URL) {
	filename := localFilename(u.Path)
	_, err := os.Stat(filename)
	if err != nil {
		t.Response.Response.Status = "404 Not Found"
		t.Response.Response.StatusCode = 404
		t.Response.BodyStr = err.Error()
		return
	}

	err = os.Remove(filename)
	if err != nil {
		t.Response.Response.Status = "403 Forbidden"
		t.Response.Response.StatusCode = 403
		t.Response.BodyStr = err.Error()
		return
	}
	t.Response.Response.Status = "200 OK"
	t.Response.Response.StatusCode = 200
	t.Response.BodyStr = fmt.Sprintf("Successfully deleted %s", u)
	t.Response.BodyErr = nil

}

// ----------------------------------------------------------------------------
// bash:// pseudo-request

type bashAction struct{}

// Schema of a bash action.
func (_ bashAction) Schema() string { return "bash" }

// Valid implements Action.Valid.
func (_ bashAction) Valid(t *Test) error {
	u := t.Request.Request.URL
	if u.Host != "" && (u.Host != "localhost" && u.Host != "127.0.0.1") { // TODO IPv6
		return fmt.Errorf("bash:// on remote host not implemented")
	}
	return nil
}

// Execute a bash script:
func (_ bashAction) Execute(t *Test) error {
	t.infof("Bash script in %q", t.Request.Request.URL.String())

	start := time.Now()
	defer func() {
		t.Response.Duration = time.Since(start)
	}()

	workDir := t.Request.Request.URL.Path

	// Save the request body to a temporary file.
	temp, err := ioutil.TempFile("", "bashscript")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	_, err = temp.WriteString(t.Request.SentBody)
	cerr := temp.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.Request.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", name)
	cmd.Dir = workDir
	for k, v := range t.Request.Params {
		if strings.Contains(k, "=") {
			t.errorf("Environment variable %q from Params contains =; dropped.", k)
			continue
		}
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v[0]))
	}

	// Fake a http.Response
	t.Response.Response = &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       nil, // already close and consumed
		Trailer:    make(http.Header),
		Request:    t.Request.Request,
	}

	// Execute cmd.
	b := bytes.Buffer{}
	cmd.Stdout = &b
	cmd.Stderr = &b
	err = cmd.Start()
	if err != nil {
		// Something is fundamentally wrong if we cannot start the
		// script at all (e.g. a nonexistent working directory).
		// As the script won't run and thus won't produce any dignostic
		// output this state is worth a test status of Error.
		return err
	}
	err = cmd.Wait()
	t.Response.BodyStr = b.String()

	if ctx.Err() == context.DeadlineExceeded {
		t.Response.Response.StatusCode = http.StatusRequestTimeout
		t.Response.Response.Status = "408 Timeout" // TODO check!
	} else if err != nil {
		// With catching errors to start the script early this code
		// path should become less likely.
		t.Response.Response.Status = "500 Internal Server Error"
		t.Response.Response.StatusCode = 500
		emsg := err.Error()
		t.Response.Response.Header.Set("Exit-Status", emsg)
		// Append error message to body too: Most likely we do not
		// have any body anyway and "normal" HTTP request add the
		// error message to the body. This hopefully helps debugging
		// too.
		if len(t.Response.BodyStr) > 0 {
			t.Response.BodyStr += "\n"
		}
		t.Response.BodyStr += emsg
	} else {
		t.Response.Response.Header.Set("Exit-Status", "exit status 0")
	}

	return nil
}

// ----------------------------------------------------------------------------
// sql:// pseudo requests

type bogusSQLQuery string

func (e bogusSQLQuery) Error() string { return string(e) }

var (
	errMissingDBDriver = bogusSQLQuery("ht: missing database driver name (host of URL) in sql:// pseudo query")
	errMissingDSN      = bogusSQLQuery("ht: missing Data-Source-Name in sql:// pseudo query")
	errMissingSQL      = bogusSQLQuery("ht: missing query (request body) in sql:// pseudo query")
)

type sqlAction struct{}

// Schema implements Action.Schema.

func (s sqlAction) Schema() string { return "sql" }

// Valid implements Action.Valid.
func (s sqlAction) Valid(t *Test) error {
	// Is the pseudoquery formally okay? All needed information available?
	if t.Request.Method != http.MethodGet && t.Request.Method != http.MethodPost {
		return bogusSQLQuery(
			fmt.Sprintf("ht: illegal method %s for sql:// pseudo query",
				t.Request.Method))
	}

	u := t.Request.Request.URL
	if u.Host == "" {
		return errMissingDBDriver
	}
	dsn := t.Request.Header.Get("Data-Source-Name")
	if dsn == "" {
		return errMissingDSN
	}
	if t.Request.Body == "" {
		return errMissingSQL
	}

	db, err := sql.Open(u.Host, dsn)
	if err != nil {
		return bogusSQLQuery(err.Error())
	}
	db.Close()

	return nil
}
func (s sqlAction) Execute(t *Test) error { return executeSQL(t) }

// executeSQL executes a SQL query:
func executeSQL(t *Test) error {
	t.infof("SQL query in %q", t.Request.Request.URL.String())

	start := time.Now()
	defer func() {
		t.Response.Duration = time.Since(start)
	}()

	u := t.Request.Request.URL
	dsn := t.Request.Header.Get("Data-Source-Name")
	db, err := sql.Open(u.Host, dsn)
	if err != nil {
		return bogusSQLQuery(err.Error())
	}

	// Fake a http.Response
	t.Response.Response = &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       nil, // already close and consumed
		Trailer:    make(http.Header),
		Request:    t.Request.Request,
	}

	ct := "application/json" // Content-Type header
	switch t.Request.Method {
	case http.MethodGet:
		accept := t.Request.Header.Get("Accept")
		t.Response.BodyStr, ct, err = sqlQuery(db, t.Request.Body, accept)
		if err != nil {
			return err
		}
	case http.MethodPost:
		t.Response.BodyStr, err = sqlExecute(db, t.Request.Body)
		if err != nil {
			return err
		}
	default:
		panic("cannot happen")
	}
	t.Response.Response.Header.Set("Content-Type", ct)

	return nil
}

// Returns a json like
//    {
//        "LastInsertId": { "Value": 1234 },
//        "RowsAffected": {
//            "Value": 0,
//            "Error": "something went wrong"
//        }
//    }
func sqlExecute(db *sql.DB, query string) (string, error) {
	result, err := db.Exec(query)
	if err != nil {
		return "", err
	}

	tmp := struct {
		LastInsertId struct {
			Value int64
			Error string `json:",omitempty"`
		}
		RowsAffected struct {
			Value int64
			Error string `json:",omitempty"`
		}
	}{}

	lii, liiErr := result.LastInsertId()
	tmp.LastInsertId.Value = lii
	if liiErr != nil {
		tmp.LastInsertId.Error = liiErr.Error()
	}

	ra, raErr := result.RowsAffected()
	tmp.RowsAffected.Value = ra
	if raErr != nil {
		tmp.RowsAffected.Error = raErr.Error()
	}
	body, err := json.MarshalIndent(tmp, "", "    ")
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// sqlQuery is invoked via GET requests and does a sql.DB.Query which
// return a set of rows. These rows are encoded according to accept and
// returned as a string.
// Allowed values for accept are:
//    application/json (default)
//    text/plain
//    text/csv
func sqlQuery(db *sql.DB, query string, accept string) (body string, contentType string, err error) {
	rows, err := db.Query(query)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", "", err
	}

	if accept == "" {
		accept = "application/json"
	}
	mediatype, params, err := mime.ParseMediaType(accept)
	if err != nil {
		return "", "", err
	}
	showHeader := false
	switch params["header"] {
	case "present", "true", "yes":
		showHeader = true
	}

	var recorder recordWriter
	switch mediatype {
	case "text/plain":
		sep := "\t"
		if s, ok := params["fieldsep"]; ok {
			sep = s
		}

		recorder = newPlaintextRecorder(sep, showHeader, columns)
	case "text/csv":
		recorder = newCSVRecorder(showHeader, columns)
	case "application/json":
		fallthrough
	default:
		recorder = newJsonRecorder(columns)
	}

	values := make([]string, len(columns))
	ptrs := make([]interface{}, len(columns))
	for i := range values {
		ptrs[i] = &values[i]
	}
	for rows.Next() {
		err = rows.Scan(ptrs...)
		if err != nil {
			bodySoFar, _ := recorder.Close()
			return bodySoFar, accept, err
		}
		recorder.WriteRecord(values)
	}
	err = rows.Err() // get any error encountered during iteration
	body, _ = recorder.Close()
	return body, accept, err
}

// ----------------------------------------------------------------------------
// Query record recorders

type recordWriter interface {
	WriteRecord([]string)
	Close() (string, error)
}

// jsonRecorder produces a JSON output from the queried database rows.
type jsonRecorder struct {
	cols  []string
	buf   *bytes.Buffer
	first bool
	tmp   map[string]string
	err   error
}

func newJsonRecorder(cols []string) *jsonRecorder {
	buf := &bytes.Buffer{}
	buf.WriteString("[\n  ")
	return &jsonRecorder{
		cols:  cols,
		buf:   buf,
		first: true,
		tmp:   make(map[string]string, len(cols)),
	}
}

func (jr *jsonRecorder) WriteRecord(values []string) {
	if jr.err != nil {
		return
	}
	for i, col := range jr.cols {
		jr.tmp[col] = values[i]
	}
	record, err := json.MarshalIndent(jr.tmp, "  ", "  ")
	if err != nil {
		jr.err = err
		return
	}
	if jr.first {
		jr.first = false
	} else {
		_, err = jr.buf.WriteString(",\n  ")
		if err != nil {
			jr.err = err
			return
		}
	}
	_, err = jr.buf.Write(record)
	if err != nil {
		jr.err = err
	}
}

func (jr *jsonRecorder) Close() (string, error) {
	_, err := jr.buf.WriteString("\n]")
	if err != nil {
		jr.err = err
	}
	return jr.buf.String(), jr.err
}

// ----------------------------------------------------------------------------
// Plaintext Record Writer

// plaintextRecorder produces plaintext from the queried rows
type plaintextRecorder struct {
	buf   *bytes.Buffer
	first bool
	sep   string
	cols  []string
}

func newPlaintextRecorder(sep string, header bool, cols []string) *plaintextRecorder {
	ptr := &plaintextRecorder{
		buf:   &bytes.Buffer{},
		first: true,
		sep:   sep,
		cols:  cols,
	}
	if header && len(cols) > 0 {
		ptr.WriteRecord(cols)
	}
	return ptr
}

func (ptr *plaintextRecorder) WriteRecord(values []string) {
	if ptr.first {
		ptr.first = false
	} else {
		ptr.buf.WriteRune('\n')
	}
	sep := ""
	for _, v := range values {
		fmt.Fprintf(ptr.buf, "%s%s", sep, v)
		sep = ptr.sep
	}
}

func (ptr *plaintextRecorder) Close() (string, error) {
	return ptr.buf.String(), nil
}

// ----------------------------------------------------------------------------
// CVS Record Writer

// csvRecorder produces a CSV output from the queried databse rows.
type csvRecorder struct {
	buf *bytes.Buffer
	csv *csv.Writer
}

func newCSVRecorder(header bool, cols []string) *csvRecorder {
	buf := &bytes.Buffer{}
	csv := csv.NewWriter(buf)
	if header {
		csv.Write(cols)
	}
	return &csvRecorder{
		buf: buf,
		csv: csv,
	}
}

func (cr *csvRecorder) WriteRecord(values []string) {
	cr.csv.Write(values)
}

func (cr *csvRecorder) Close() (string, error) {
	cr.csv.Flush()
	return cr.buf.String(), cr.csv.Error()
}

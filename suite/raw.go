// Copyright 2016 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package suite

import (
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
	"strings"

	"github.com/vdobler/ht/cookiejar"
	"github.com/vdobler/ht/errorlist"
	"github.com/vdobler/ht/ht"
	"github.com/vdobler/ht/internal/hjson"
	"github.com/vdobler/ht/mock"
	"github.com/vdobler/ht/populate"
	"github.com/vdobler/ht/scope"
)

func pp(msg string, v interface{}) {
	data, err := hjson.Marshal(v)
	fmt.Println(msg, string(data), err)
}

// ----------------------------------------------------------------------------
//   File

// File is a textual representation of a hjson data read from disk.
type File struct {
	Data string
	Name string
}

// LoadFile reads the given file and returns it as a File.
func LoadFile(filename string) (*File, error) {
	filename = filepath.ToSlash(filename)
	filename = path.Clean(filename)
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	// Make sure this is decodable HJSON.
	var soup interface{}
	err = hjson.Unmarshal(data, &soup)
	if err != nil {
		return nil, fmt.Errorf("file %s not valid hjson: %s", filename, err)
	}

	return &File{
		Data: string(data),
		Name: filepath.ToSlash(filename),
	}, nil

}

// Dirname of f.
func (f *File) Dirname() string {
	return path.Dir(f.Name)
}

// Basename of f.
func (f *File) Basename() string {
	return path.Base(f.Name)
}

// decode f which must be a hjson file to a map[string]interface{} soup.
func (f *File) decode() (map[string]interface{}, error) {
	var soup interface{}
	err := hjson.Unmarshal([]byte(f.Data), &soup)
	if err != nil {
		return nil, fmt.Errorf("file %s is not valid hjson: %s", f.Name, err)
	}
	m, ok := soup.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("file %s is not an object (got %T)", f.Name, soup)
	}
	return m, nil
}

// populate x with the decoded f, ignoring excess properties.
func (f *File) decodeLaxTo(x interface{}) error {
	var soup interface{}
	err := hjson.Unmarshal([]byte(f.Data), &soup)
	if err != nil {
		return fmt.Errorf("file %s is not valid hjson: %s", f.Name, err)
	}
	m, ok := soup.(map[string]interface{})
	if !ok {
		return fmt.Errorf("file %s is not an object (got %T)", f.Name, soup)
	}
	err = populate.Lax(x, m)
	if err != nil {
		return fmt.Errorf("error decoding file %s: %s", f.Name, err)
	}

	return nil
}

// populate x with the decoded f. Top level properties in in drop are
// dropped before attempting a strict population
func (f *File) decodeStrictTo(x interface{}, drop []string) error {
	var soup interface{}
	err := hjson.Unmarshal([]byte(f.Data), &soup)
	if err != nil {
		return fmt.Errorf("file %s is not valid hjson: %s", f.Name, err)
	}
	m, ok := soup.(map[string]interface{})
	if !ok {
		return fmt.Errorf("file %s is not an object (got %T)", f.Name, soup)
	}
	for _, d := range drop {
		delete(m, d)
	}
	err = populate.Strict(x, m)
	if err != nil {
		return fmt.Errorf("error decoding file %s: %s", f.Name, err)
	}

	return nil
}

// ----------------------------------------------------------------------------
//   Mixin

// Mixin of a test.
type Mixin struct {
	*File
}

// LoadMixin with the given filename.
func loadMixin(filename string, fs FileSystem) (*Mixin, error) {
	file, err := fs.Load(filename)
	if err != nil {
		return nil, err
	}

	return &Mixin{File: file}, nil
}

// ----------------------------------------------------------------------------
//   RawTest

// RawTest is a raw for of a test as read from disk with its mixins
// and its variables.
type RawTest struct {
	*File
	Mixins      []*Mixin          // Mixins of this test.
	Variables   map[string]string // Variables are the defaults of the variables.
	contextVars map[string]string
	mocks       []*RawMock
	disabled    bool
}

func (rt *RawTest) String() string {
	return rt.File.Name
}

// Disable rt.
func (rt *RawTest) Disable() { rt.disabled = true }

// Enable rt.
func (rt *RawTest) Enable() { rt.disabled = false }

// IsEnabled reports if rt is enabled.
func (rt *RawTest) IsEnabled() bool { return !rt.disabled }

// LoadRawTest reads filename and produces a new RawTest.
func LoadRawTest(filename string, fs FileSystem) (*RawTest, error) {
	raw, err := fs.Load(filename)
	if err != nil {
		return nil, err
	}

	// Unmarshal to find the Mixins and Variables
	x := &struct {
		Mixin     []string
		Variables map[string]string
	}{}
	err = raw.decodeLaxTo(x)
	if err != nil {
		return nil, err
	}

	// Load all mixins from disk.
	testdir := raw.Dirname()
	mixins, err := loadMixins(x.Mixin, testdir, fs)
	if err != nil {
		return nil, err
	}

	return &RawTest{
		File:      raw,
		Mixins:    mixins,
		Variables: x.Variables,
	}, nil
}

func loadMixins(mixs []string, dir string, fs FileSystem) ([]*Mixin, error) {
	mixins := []*Mixin{}
	for _, file := range mixs {
		mixpath := path.Join(dir, file)
		mixin, err := loadMixin(mixpath, fs)
		if err != nil {
			return nil, fmt.Errorf("cannot load mixin %q: %s",
				file, err)
		}
		mixins = append(mixins, mixin)
	}
	return mixins, nil
}

// ToTest produces a ht.Test from a raw test rt.
func (rt *RawTest) ToTest(variables scope.Variables) (*ht.Test, error) {
	bogus := &ht.Test{Result: ht.Result{Status: ht.Bogus}}
	replacer := variables.Replacer()

	// Make substituted a copy of rt with variables substituted.
	// Dropping the Variabels field as this is no longer useful.
	substituted := &RawTest{
		File: &File{
			Data: replacer.Replace(rt.File.Data),
			Name: rt.File.Name,
		},
		Mixins: make([]*Mixin, len(rt.Mixins)),
	}
	for i := range rt.Mixins {
		substituted.Mixins[i] = &Mixin{
			File: &File{
				Data: replacer.Replace(rt.Mixins[i].File.Data),
				Name: rt.Mixins[i].File.Name,
			},
		}
	}

	test, err := substituted.toTest(variables)
	if err != nil {
		return bogus, err
	}

	mixins := make([]*ht.Test, len(substituted.Mixins))
	for i, rawmix := range substituted.Mixins {
		mix, err := rawmix.toTest()
		if err != nil {
			return bogus, err
		}
		mixins[i] = mix
	}

	origname, origdescr, origfollow := test.Name, test.Description, test.Request.FollowRedirects
	all := append([]*ht.Test{test}, mixins...)
	merged, err := ht.Merge(all...)
	if err != nil {
		return bogus, err
	}
	// Beautify name and description and force follow redirect
	// policy: BasedOn is not a merge between equal partners.
	merged.Description = origdescr
	merged.Name = origname
	merged.Request.FollowRedirects = origfollow

	return merged, nil
}

func (m *Mixin) toTest() (*ht.Test, error) {
	rt := &RawTest{
		File: &File{
			Data: m.File.Data,
			Name: m.Name,
		},
	}
	return rt.toTest(nil)
}

func (rt *RawTest) toTest(variables map[string]string) (*ht.Test, error) {
	m, err := rt.File.decode()
	if err != nil {
		return nil, err
	}

	delete(m, "Mixin")
	// delete(m, "Variables")
	test := &ht.Test{}

	err = populate.Strict(test, m)
	if err != nil {
		return nil, err // better error message here
	}
	test.Variables = make(map[string]string, len(variables))
	for n, v := range variables {
		test.Variables[n] = v
	}

	return test, nil
}

// ----------------------------------------------------------------------------
//   RawSuite

// RawElement represents one test in a RawSuite.
type RawElement struct {
	File      string
	Variables map[string]string
	Mocks     []string

	Test map[string]interface{}
}

// RawSuite represents a suite as represented on disk as a HJSON file.
type RawSuite struct {
	*File
	Name, Description     string
	Setup, Main, Teardown []RawElement
	KeepCookies           bool
	OmitChecks            bool
	Variables             map[string]string
	Verbosity             int

	tests []*RawTest
}

// RawTests return all tests in rs.
func (rs *RawSuite) RawTests() []*RawTest {
	return rs.tests
}

// AddRawTests adds ts to the tests in rs.
func (rs *RawSuite) AddRawTests(ts ...*RawTest) {
	rs.tests = append(rs.tests, ts...)
}

func parseRawSuite(name string, txt string) (*RawSuite, error) {
	fs, err := NewFileSystem(txt)
	if err != nil {
		return nil, err
	}

	return LoadRawSuite(name, fs)
}

// LoadRawSuite with the given filename from fs.
func LoadRawSuite(filename string, fs FileSystem) (*RawSuite, error) {
	raw, err := fs.Load(filename)
	if err != nil {
		return nil, err
	}

	rs := &RawSuite{}
	err = raw.decodeStrictTo(rs, nil)
	if err != nil {
		return nil, err // better error message here
	}
	rs.File = raw // re-set as decodeStritTo clears rs
	dir := rs.File.Dirname()
	load := func(elems []RawElement, which string) error {
		for i, elem := range elems {
			var err error
			var rt *RawTest
			var filename string
			if elem.File != "" {
				filename = path.Join(dir, elem.File)
				rt, err = LoadRawTest(filename, fs)
				if err != nil {
					return fmt.Errorf("cannot load test %q (%d. %s): %s",
						filename, i+1, which, err)
				}
			} else if len(elem.Test) != 0 {
				name := fmt.Sprintf("%s_inline-%d.%s",
					rs.File.Name, i+1, which)
				rt, err = rawTestFromInline(name, dir, fs, elem.Test)
				if err != nil {
					return fmt.Errorf("cannot parse inline test (%d. %s): %s",
						i+1, which, err)

				}
			} else {
				return fmt.Errorf("File and Test must not both be empty in %d. %s", i+1, which)
			}
			rt.contextVars = elem.Variables
			for _, mockname := range elem.Mocks {
				mf, err := LoadRawMock(path.Join(dir, mockname), fs)
				if err != nil {
					return fmt.Errorf("cannot instantiate test (%d. %s): cannot load mock %q: %s",
						i+1, which, mockname, err)
				}
				rt.mocks = append(rt.mocks, mf)
			}
			rs.tests = append(rs.tests, rt)
		}
		return nil
	}
	err = load(rs.Setup, "Setup")
	if err != nil {
		return nil, err
	}
	err = load(rs.Main, "Main")
	if err != nil {
		return nil, err
	}
	err = load(rs.Teardown, "Teardown")
	if err != nil {
		return nil, err
	}

	return rs, nil
}

func rawTestFromInline(name, dir string, fs FileSystem, inline map[string]interface{}) (*RawTest, error) {
	mixins := []*Mixin{}
	if m, ok := inline["Mixins"]; ok {
		mixs := []string{}
		err := populate.Strict(&mixs, m)
		if err != nil {
			fmt.Println("Mixins issue", err)
			return nil, err
		}
		delete(inline, "Mixins")
		mixins, err = loadMixins(mixs, dir, fs)
		if err != nil {
			return nil, err
		}
	}

	b, err := hjson.Marshal(inline)
	if err != nil {
		return nil, err
	}

	raw := &File{
		Data: string(b),
		Name: name,
	}

	return &RawTest{
		File:   raw,
		Mixins: mixins,
	}, nil
}

// Validate rs to make sure it can be decoded into welformed ht.Tests.
func (rs *RawSuite) Validate(global map[string]string) error {
	suiteScope := scope.New(global, rs.Variables, true)
	suiteScope["SUITE_DIR"] = rs.File.Dirname()
	suiteScope["SUITE_NAME"] = rs.File.Basename()

	el := errorlist.List{}
	for _, rt := range rs.tests {
		callScope := scope.New(suiteScope, rt.contextVars, true)
		testScope := scope.New(callScope, rt.Variables, false)
		testScope["TEST_DIR"] = rt.File.Dirname()
		testScope["TEST_NAME"] = rt.File.Basename()
		_, err := rt.ToTest(testScope)
		if err != nil {
			err := fmt.Errorf("invalid test %s (included by %s): %s",
				rt.File.Name, rs.File.Name, err)
			el = append(el, err)
		}
	}
	if len(el) > 0 {
		return el
	}

	return nil
}

// Execute the raw suite rs and capture the outcome in a Suite.
//
// Tests are executed linearely, first the Setup, then the Main and finally
// the Teardown test.  Any Failure or error during setup will skip further
// setup and main test (but allteardown will be executed). The following
// table shows two runs with possible outcomes.
//
//      Test Type     Run1     Run2
//    ------------------------------------------
//      Setup-1       Pass     Pass
//      Setup-2       Fail     Pass
//      Setup-3       Skip     Pass
//      Main-1        Skip     Fail
//      Main-2        Skip     Pass
//      Teardown-1    Pass     Pass
//      Teardown-2    Fail     Error
//      Teardown-3    Pass     Pass
func (rs *RawSuite) Execute(global map[string]string, jar *cookiejar.Jar, logger ht.Logger) *Suite {
	suite := NewFromRaw(rs, global, jar, logger)
	N := len(rs.tests)
	setup, main, teardown := len(rs.Setup), len(rs.Main), len(rs.Teardown)
	i := 0
	isSetup := func() bool { return i <= setup }
	isMain := func() bool { return i > setup && i <= setup+main }
	isSetupOrMain := func() bool { return i <= setup+main }
	setupfailures := false

	executor := func(test *ht.Test) error {
		i++
		if isSetup() {
			test.SetMetadata("SeqNo", fmt.Sprintf("Setup-%02d", i))
		} else if isMain() {
			test.SetMetadata("SeqNo", fmt.Sprintf("Main-%02d", i-setup))
		} else {
			test.SetMetadata("SeqNo", fmt.Sprintf("Teardown-%02d", i-setup-main))
		}

		switch {
		case test.Result.Status == ht.Skipped:
			fallthrough
		case !rs.tests[i-1].IsEnabled():
			fallthrough
		case setupfailures && isSetupOrMain():
			test.Result.Status = ht.Skipped
			return nil
		}

		if test.Result.Status != ht.Bogus {
			// Run only non-bogus tests.
			test.Execution.Verbosity = rs.Verbosity
			test.Run()
		}
		if test.Result.Status > ht.Pass && isSetup() {
			setupfailures = true
		}

		return nil
	}

	// Overall Suite status is computetd from Setup and Main tests only.
	suite.Iterate(executor)
	status := ht.NotRun
	errors := errorlist.List{}
	for i := 0; i < N-teardown && i < len(suite.Tests); i++ {
		if ts := suite.Tests[i].Result.Status; ts > status {
			status = ts
		}
		if err := suite.Tests[i].Result.Error; err != nil {
			errors = append(errors, err)
		}
	}

	suite.Status = status
	if len(errors) == 0 {
		suite.Error = nil
	} else {
		suite.Error = errors
	}

	return suite
}

// ----------------------------------------------------------------------------
// FileSystem

// FileSystem acts like an in-memory filesystem.
// A empty FileSystem accesses the real OS file system.
type FileSystem map[string]*File

// Load the file with the given name from fs.
func (fs FileSystem) Load(name string) (*File, error) {
	if len(fs) == 0 {
		return LoadFile(name)
	}
	if f, ok := fs[name]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("file %s not found", name)
}

// NewFileSystem parses txt which must be of the form
//
//     # <filename1>
//     <filecontent1>
//
//     # <filename2>
//     <filecontent2>
//
//     ...
//
// into a new FileSystem.
func NewFileSystem(txt string) (FileSystem, error) {
	txt = "\n" + txt
	parts := strings.Split(txt, "\n#")
	filesystem := make(FileSystem, len(parts))

	for i, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) == 0 {
			continue
		}
		sp := strings.SplitN(part, "\n", 2)
		name := strings.TrimSpace(sp[0])

		if len(sp) != 2 || len(name) == 0 {
			return nil, fmt.Errorf("malformed part %d", i+1)
		}
		if _, ok := filesystem[name]; ok {
			return nil, fmt.Errorf("duplicate name %q", name)
		}
		filesystem[name] = &File{
			Name: name,
			Data: sp[1],
		}
	}

	return filesystem, nil
}

// ----------------------------------------------------------------------------
// RawScenrio & RawLoadtest

// RawScenario represents a scenario in a load test.
type RawScenario struct {
	Name       string            // Name of this Scenario
	File       string            // File is the RawSuite to use as scenario
	Percentage int               // Percentage this scenario contributes to the load test.
	MaxThreads int               // MaxThreads to use for this scenario. 0 means unlimited.
	Variables  map[string]string // Variables used.
	OmitChecks bool              // OmitChecks in the tests.

	rawSuite *RawSuite
}

// RawLoadTest as read from disk.
type RawLoadTest struct {
	*File
	Name        string
	Description string
	Scenarios   []RawScenario
	Variables   map[string]string
}

func parseRawLoadtest(name string, txt string) (*RawLoadTest, error) {
	fs, err := NewFileSystem(txt)
	if err != nil {
		return nil, err
	}

	return LoadRawLoadtest(name, fs)
}

// LoadRawLoadtest from the given filename.
func LoadRawLoadtest(filename string, fs FileSystem) (*RawLoadTest, error) {
	raw, err := fs.Load(filename)
	if err != nil {
		return nil, err
	}

	rlt := &RawLoadTest{}
	err = raw.decodeStrictTo(rlt, nil)
	if err != nil {
		return nil, err // better error message here
	}
	rlt.File = raw // re-set as decodeStritTo clears rs
	dir := rlt.File.Dirname()

	for i, s := range rlt.Scenarios {
		if s.File != "" {
			filename := path.Join(dir, s.File)
			rs, err := LoadRawSuite(filename, fs)
			if err != nil {
				return nil, fmt.Errorf("cannot load suite %q (%d. scenario): %s",
					filename, i+1, err)
			}
			rlt.Scenarios[i].rawSuite = rs
		} else {
			panic("File must not be empty")
		}
	}

	return rlt, nil
}

// ToScenario produces a list of scenarios from raw.
func (raw *RawLoadTest) ToScenario(globals map[string]string) []Scenario {
	scenarios := []Scenario{}
	ltscope := scope.New(globals, raw.Variables, true)
	for _, rs := range raw.Scenarios {
		callscope := scope.New(ltscope, rs.Variables, true)
		scen := Scenario{
			Name:       rs.Name,
			RawSuite:   rs.rawSuite,
			Percentage: rs.Percentage,
			MaxThreads: rs.MaxThreads,
			globals:    callscope,
		}

		scenarios = append(scenarios, scen)
	}

	return scenarios
}

// ----------------------------------------------------------------------------
// Mocks

// RawMock is the raw form of a mock read in from disk.
type RawMock struct {
	*File

	// Variables are the defaults of the variables.
	Variables map[string]string
}

// LoadRawMock tries to reads filename from fs as a RawMock.
func LoadRawMock(filename string, fs FileSystem) (*RawMock, error) {
	raw, err := fs.Load(filename)
	if err != nil {
		return nil, err
	}

	// Unmarshal to findVariables.
	x := &struct {
		Variables map[string]string
	}{}
	err = raw.decodeLaxTo(x)
	if err != nil {
		return nil, err // better error message here
	}

	return &RawMock{
		File:      raw,
		Variables: x.Variables,
	}, nil
}

// ToMock produces a real Mock from rm. The auto flag triggers generation of
// COUNTER and RANDOM variables.
func (rm *RawMock) ToMock(variables scope.Variables, auto bool) (*mock.Mock, error) {
	vars := scope.New(variables, rm.Variables, auto)
	replacer := vars.Replacer()

	substituted := &File{
		Data: replacer.Replace(rm.File.Data),
		Name: rm.File.Name,
	}

	var m = &mock.Mock{}
	if err := substituted.decodeStrictTo(m, nil); err != nil {
		return nil, err
	}

	// Copy scope to mock for use in @vbody et al.
	m.Variables = vars.Copy()

	return m, nil
}

// LoadMock loads filename from fs and replaces the variables. If auto is
// true the automatic variables COUNTER and RANDOM are set too.
func LoadMock(filename string, fs FileSystem, variables map[string]string, auto bool) (*mock.Mock, error) {
	raw, err := LoadRawMock(filename, fs)
	if err != nil {
		return nil, err
	}
	return raw.ToMock(variables, auto)
}

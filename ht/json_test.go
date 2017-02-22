// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ht

import (
	"fmt"
	"testing"
)

var jr = Response{BodyStr: `{"foo": 5, "bar": [1,2,3]}`}
var ar = Response{BodyStr: `["jo nesbo",["jo nesbo","jo nesbo harry hole","jo nesbo sohn","jo nesbo koma","jo nesbo hörbuch","jo nesbo headhunter","jo nesbo pupspulver","jo nesbo leopard","jo nesbo schneemann","jo nesbo the son"],[{"nodes":[{"name":"Bücher","alias":"stripbooks"},{"name":"Trade-In","alias":"tradein-aps"},{"name":"Kindle-Shop","alias":"digital-text"}]},{}],[]]`}
var jre = Response{BodyStr: `{"foo": 5, "bar": [1,"qux",3], "waz": true, "nil": null, "uuid": "ad09b43c-6538-11e6-8b77-86f30ca893d3"}`}
var jrx = Response{BodyStr: `{"foo": 5, "blub...`}
var jrs = Response{BodyStr: `"foo"`}
var jri = Response{BodyStr: `123`}
var jrf = Response{BodyStr: `45.67`}
var jrm = Response{BodyStr: `"{\"foo\":5,\"bar\":[1,2,3]}"`} // "modern JSON"

var jsonExpressionTests = []TC{
	{jr, &JSONExpr{Expression: "(.foo == 5) && ($len(.bar)==3) && (.bar[1]==2)"}, nil},
	{jr, &JSONExpr{Expression: "$max(.bar) == 3"}, nil},
	{jr, &JSONExpr{Expression: "$has(.bar, 2)"}, nil},
	{jr, &JSONExpr{Expression: "$has(.bar, 7)"}, someError},
	{jr, &JSONExpr{Expression: ".foo == 3"}, someError},
	{ar, &JSONExpr{Expression: "$len(.) > 3"}, nil},
	{ar, &JSONExpr{Expression: "$len(.) == 4"}, nil},
	{ar, &JSONExpr{Expression: ".[0] == \"jo nesbo\""}, nil},
	{ar, &JSONExpr{Expression: "$len(.[1]) == 10"}, nil},
	{ar, &JSONExpr{Expression: ".[1][6] == \"jo nesbo pupspulver\""}, nil},
}

func TestJSONExpression(t *testing.T) {
	for i, tc := range jsonExpressionTests {
		runTest(t, i, tc)
	}
}

var jsonConditionTests = []TC{
	{jr, &JSON{Element: "foo", Condition: Condition{Equals: "5"}}, nil},
	{jr, &JSON{Element: "bar.1", Condition: Condition{Equals: "2"}}, nil},
	{jr, &JSON{Element: "bar.2"}, nil},
	{jr, &JSON{Element: "bar#1", Sep: "#", Condition: Condition{Equals: "2"}}, nil},
	{jr, &JSON{Element: "foo", Condition: Condition{Equals: "bar"}}, someError},
	{jr, &JSON{Element: "bar.5"}, fmt.Errorf("no index 5 in array bar of len 3")},
	{jr, &JSON{Element: "bar.3", Condition: Condition{Equals: "2"}}, someError},
	{jr, &JSON{Element: "foo.wuz", Condition: Condition{Equals: "bar"}}, someError},
	{jr, &JSON{Element: "qux", Condition: Condition{Equals: "bar"}},
		fmt.Errorf("element qux not found")},

	{ar, &JSON{Element: "0", Condition: Condition{Equals: `"jo nesbo"`}}, nil},
	{ar, &JSON{Element: "1.4", Condition: Condition{Contains: `jo nesbo`}}, nil},
	{ar, &JSON{Element: "2.0.nodes.2.name", Condition: Condition{Equals: `"Kindle-Shop"`}}, nil},

	{jre, &JSON{Element: "bar.1", Condition: Condition{Equals: `"qux"`}}, nil},
	{jre, &JSON{Element: "waz", Condition: Condition{Equals: `true`}}, nil},
	{jre, &JSON{Element: "nil", Condition: Condition{Equals: `null`}}, nil},
	{jre, &JSON{Element: "nil", Condition: Condition{Prefix: `"`}}, someError},
	{jre, &JSON{Element: "uuid", Condition: Condition{
		Regexp: `^"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"$`}}, nil},

	{jre, &JSON{}, nil},
	{jrx, &JSON{}, someError},

	{jri, &JSON{Element: ".", Condition: Condition{Equals: "123"}}, nil},
	{jrf, &JSON{Element: ".", Condition: Condition{Equals: "45.67"}}, nil},
	{jrs, &JSON{Element: ".", Condition: Condition{Contains: `foo`}}, nil},

	{jrm, &JSON{Element: ".",
		Embedded: &JSON{Element: "foo", Condition: Condition{Equals: "5"}}},
		nil},
	{jrm, &JSON{Element: ".",
		Embedded: &JSON{Element: "bar.1", Condition: Condition{Equals: "2"}}},
		nil},
	{jrm, &JSON{Element: ".",
		Embedded: &JSON{Element: "bar.1", Condition: Condition{Equals: "XX"}}},
		someError},
}

func TestJSONCondition(t *testing.T) {
	for i, tc := range jsonConditionTests {
		runTest(t, i, tc)
	}
}

var findJSONelementTests = []struct {
	doc  string
	elem string
	want string
	err  string
}{
	// Primitive types
	{`123`, "", `123`, ""},
	{`123`, ".", `123`, ""},
	{`-123.456`, "", `-123.456`, ""},
	{`-123.456`, ".", `-123.456`, ""},
	{`"abc"`, "", `"abc"`, ""},
	{`"abc"`, ".", `"abc"`, ""},
	{`null`, "", `null`, ""},
	{`null`, ".", `null`, ""},
	{`true`, "", `true`, ""},
	{`true`, ".", `true`, ""},
	{`false`, "", `false`, ""},
	{`false`, ".", `false`, ""},
	{`123`, "X", `123`, "element X not found"},

	// Whole (non-primitive) documents
	{`[3, 1 , 4, 1  ]`, "", `[3, 1 , 4, 1  ]`, ""},
	{`[3, 1 , 4, 1  ]`, ".", `[3, 1 , 4, 1  ]`, ""},
	{`{"A" : 123 , "B": "foo"} `, "", `{"A" : 123 , "B": "foo"} `, ""},
	{`{"A" : 123 , "B": "foo"} `, ".", `{"A" : 123 , "B": "foo"} `, ""},

	// Arrays
	{`[3, 1, 4, 1]`, "2", `4`, ""},
	{`[3, 1, "foo", 1]`, "2", `"foo"`, ""},
	{`[3, 1, 4, 1]`, "7", ``, "no index 7 in array  of len 4"},
	{`[3, 1, 4, 1]`, "-7", ``, "no index -7 in array  of len 4"},
	{`[3, 1, 4, 1]`, "foo", ``, "foo is not a valid index"},
	{`[3, 1, 4, 1]`, "2e0", ``, "2e0 is not a valid index"},
	{`{"A":{"B":[1,2,3]}}`, "A.B.5", ``, "no index 5 in array A.B of len 3"},

	// Objects
	{`{"A": 123, "B": "foo", "C": true, "D": null}`, "A", `123`, ""},
	{`{"A": 123, "B": "foo", "C": true, "D": null}`, "B", `"foo"`, ""},
	{`{"A": 123, "B": "foo", "C": true, "D": null}`, "C", `true`, ""},
	{`{"A": 123, "B": "foo", "C": true, "D": null}`, "D", `null`, ""},
	{`{"A": 123, "B": "foo", "C": true, "D": null}`, "E", ``, "element E not found"},

	// Nested stuff
	{`{"A": [0, 1, {"B": true, "C": 2.72}, 3]}`, "A.2.C", `2.72`, ""},
	{`{"A": [0, 1, {"B": true, "C": 2.72}, 3]}`, ".A...2.C..", `2.72`, ""},
	{`{"a":{"b":{"c":{"d":77}}}}`, "a.b.c.d", `77`, ""},
	{`{"a":{"b":{"c":{"d":77}}}}`, "a.b.c", `{"d":77}`, ""},
	{`{"a":{"b":{"c":{"d":77}}}}`, "a.b", `{"c":{"d":77}}`, ""},
	{`{"a":{"b":{"c":{"d":77}}}}`, "a.b.c.d.X", ``, "element a.b.c.d.X not found"},
	{`{"a":{"b":{"c":{"d":77}}}}`, "a.b.c.X", ``, "element a.b.c.X not found"},
	{`{"a":{"b":{"c":{"d":77}}}}`, "a.b.X", ``, "element a.b.X not found"},

	// Ill-formed JSON
	{`{"A":[{"B":flop}]}`, "", `{"A":[{"B":flop}]}`, ""},
	{`{"A":[{"B":flop}]}`, ".", `{"A":[{"B":flop}]}`, ""},
	{`{"A":[{"B":flop}]}`, "A.0.B", ``, "invalid character 'l' in literal false (expecting 'a')"},
	{`{"A":[{"B":3..1..4}]}`, "A.0.B", ``, "invalid character '.' after decimal point in numeric literal"},
}

func TestFindJSONElement(t *testing.T) {
	for i, tc := range findJSONelementTests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			raw, err := findJSONelement([]byte(tc.doc), tc.elem, ".")
			if err != nil {
				if eg := err.Error(); eg != tc.err {
					t.Errorf("%d: %s from %s: got error %q, want %q",
						i, tc.elem, tc.doc, eg, tc.err)
				}
			} else {
				if tc.err != "" {
					t.Errorf("%d: %s from %s: got nil error, want %q",
						i, tc.elem, tc.doc, tc.err)
				} else {
					if got := string(raw); got != tc.want {
						t.Errorf("%d: %s from %s: got %q, want %q",
							i, tc.elem, tc.doc, got, tc.want)
					}
				}
			}
		})
	}

}

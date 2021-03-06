// Copyright 2017 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gui

import (
	"encoding/hex"
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vdobler/ht/ht"
)

func indent(depth int) string {
	return strings.Repeat("    ", depth)
}

func (v *Value) renderMessages(path string, depth int) {
	msgs := v.Messages[path]
	if len(msgs) == 0 {
		return
	}

	for _, m := range msgs {
		v.printf("%s<p class=\"msg-%s\">%s</p>\n",
			indent(depth),
			template.HTMLEscapeString(m.Type),
			template.HTMLEscapeString(m.Text))
	}
}

// ----------------------------------------------------------------------------
// Recursive rendering of HTML form

// render down val, emitting HTML to buf.
// Path is the prefix to the current input name.
func (v *Value) render(path string, depth int, readonly bool, val reflect.Value) error {
	// Display-only types:
	switch val.Type() {
	case urlURLType, htStatusType:
		return v.renderDisplayString(path, depth, readonly, val)
	}

	switch val.Kind() {
	case reflect.Bool:
		return v.renderBool(path, depth, readonly, val)
	case reflect.String:
		return v.renderString(path, depth, readonly, val)
	case reflect.Int64:
		if isDuration(val) {
			return v.renderDuration(path, depth, readonly, val)
		}
		fallthrough
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return v.renderInt(path, depth, readonly, val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64:
		return v.renderUint(path, depth, readonly, val)
	case reflect.Float32, reflect.Float64:
		return v.renderFloat64(path, depth, readonly, val)
	case reflect.Struct:
		switch val.Type() {
		case timeTimeType:
			return v.renderTime(path, depth, readonly, val)
		}
		return v.renderStruct(path, depth, readonly, val)
	case reflect.Map:
		return v.renderMap(path, depth, readonly, val)
	case reflect.Slice:
		return v.renderSlice(path, depth, readonly, val)
	case reflect.Ptr:
		return v.renderPtr(path, depth, readonly, val)
	case reflect.Interface:
		if val.Type() == errorType {
			return v.renderError(path, depth, readonly, val)
		}
		return v.renderInterface(path, depth, readonly, val)
	}

	fmt.Println("gui: won't render", val.Kind().String(), "in", path)

	return nil
}

var (
	errorType    = reflect.TypeOf((*error)(nil)).Elem()
	urlURLType   = reflect.TypeOf(&url.URL{})
	timeTimeType = reflect.TypeOf(time.Time{})
	htStatusType = reflect.TypeOf(ht.Status(0))
)

// TODO: should is{Duration,Time} should check for convertible-to-time.Time ?

func isDuration(v reflect.Value) bool {
	t := v.Type()
	return (t.PkgPath() == "time" && t.Name() == "Duration") ||
		(t.PkgPath() == "github.com/vdobler/ht/ht" && t.Name() == "Duration")
}

func isTime(v reflect.Value) bool {
	t := v.Type()
	return t.PkgPath() == "time" && t.Name() == "Time"
}

// ----------------------------------------------------------------------------
// Primitive Types

func (v *Value) renderBool(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	v.printf("%s", indent(depth))

	if readonly {
		if val.Bool() {
			v.printf("&#x2611;\n") // ☑
		} else {
			v.printf("&#x2610;\n") // ☐
		}
		return nil
	}

	checked := ""
	if val.Bool() {
		checked = " checked"
	}
	v.printf("<input type=\"checkbox\" name=\"%s\" value=\"true\" %s/>\n",
		template.HTMLEscapeString(path), checked)

	return nil
}

func (v *Value) renderInt(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	v.printf("%s", indent(depth))

	if readonly {
		v.printf("%d", val.Int())
		return nil
	}

	v.printf("<input type=\"number\" name=\"%s\" value=\"%d\" />\n",
		template.HTMLEscapeString(path),
		val.Int())

	return nil
}

func (v *Value) renderUint(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	v.printf("%s", indent(depth))

	if readonly {
		v.printf("%d", val.Uint())
		return nil
	}

	v.printf("<input type=\"number\" name=\"%s\" value=\"%d\" />\n",
		template.HTMLEscapeString(path),
		val.Uint())

	return nil
}

func (v *Value) renderFloat64(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	v.printf("%s", indent(depth))

	if readonly {
		v.printf("%f", val.Float())
		return nil
	}

	v.printf("<input type=\"number\" name=\"%s\" value=\"%f\" step=\"any\"/>\n",
		template.HTMLEscapeString(path),
		val.Float())

	return nil
}

// ----------------------------------------------------------------------------
// Strings

func (v *Value) renderString(path string, depth int, readonly bool, val reflect.Value) error {
	str := val.String()
	if data := binaryString(str); data != nil {
		return v.renderBinaryData(path, depth, readonly, data)
	}

	v.renderMessages(path, depth)
	v.printf("%s", indent(depth))

	isMultiline := strings.Contains(str, "\n") || len(str) > 200
	escVal := template.HTMLEscapeString(str)
	if readonly {
		if isMultiline {
			v.printf("<pre>")
			for _, line := range strings.Split(val.String(), "\n") {
				v.printf("%s\n", template.HTMLEscapeString(line))
			}
			v.printf("</pre>\n")
		} else {
			v.printf("<code>%s</code>\n", escVal)
		}
		return nil
	}

	if v.nextfieldinfo.Multiline || isMultiline {
		v.printf("<textarea cols=\"82\" rows=\"5\" name=\"%s\">%s</textarea>\n",
			template.HTMLEscapeString(path),
			escVal)
	} else if len(v.nextfieldinfo.Only) > 0 {
		v.printf("<select name=\"%s\">\n", template.HTMLEscapeString(path))
		current := val.String()
		for _, only := range v.nextfieldinfo.Only {
			selected := ""
			if current == only {
				selected = ` selected="selected"`
			}
			v.printf("%s<option%s>%s</option>\n",
				indent(depth+1),
				selected,
				template.HTMLEscapeString(only))
		}
		v.printf("%s</select>\n", indent(depth))
	} else if len(v.nextfieldinfo.Any) > 0 {
		current := " " + val.String() + " "
		name := template.HTMLEscapeString(path)
		for _, any := range v.nextfieldinfo.Any {
			option := template.HTMLEscapeString(any)
			checked := ""
			if strings.Contains(current, " "+any+" ") {
				checked = `checked`
			}
			v.printf("%s<input type=\"checkbox\" name=\"%s\" value=\"%s\" %s/>&nbsp;%s &emsp; \n",
				indent(depth+1),
				name,
				option,
				checked,
				option)
		}
		// This hidden "checkbox" force the browser to send at least
		// this one parameter: If you uncheck all checkboxes the browser
		// would not send parameter name at all so we would miss the
		// update.
		v.printf("%s<input type=\"hidden\" name=\"%s\" value=\"\"/>\n",
			indent(depth+1), name)

	} else {
		v.printf("<input type=\"text\" name=\"%s\" value=\"%s\" />\n",
			template.HTMLEscapeString(path),
			escVal)
	}
	return nil
}

// binaryString returns a non-nil byte slice if str is not suitable for
// rendering into HTML, that is if non-UTF-8, contains invalid characters
// or is some non-text format.
func binaryString(str string) []byte {
	data := []byte(str)

	// Make sure s is valid UTF-8 and valid HTML text as defined in
	// https://www.w3.org/TR/html51/dom.html#text
	s := data
	for len(s) > 0 {
		r, size := utf8.DecodeRune(s)
		s = s[size:]
		if r == utf8.RuneError {
			return data // Invalid UTF-8
		}
		if r == 0 || r == 0xFEFF { // I hate BOMs
			return data
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r' {
			// Space is okay. See
			// https://www.w3.org/TR/html51/infrastructure.html#space-characters
			continue
		}
		if (r >= 0 && r <= 0xD7FF16) || (r >= 0xE00016 && r <= 0x10FFFF) {
			// Scalar values are okay. See
			// https://www.w3.org/TR/html51/infrastructure.html#unicode-character
			// http://unicode.org/glossary/#unicode_scalar_value
			continue
		}
		return data
	}
	ct := http.DetectContentType(data)
	mt, _, _ := mime.ParseMediaType(ct)

	if strings.HasPrefix(mt, "text/") {
		return nil
	}
	return data
}

func (v *Value) renderBinaryData(path string, depth int, readonly bool, data []byte) error {
	v.renderMessages(path, depth)

	hexdump := hex.Dump(data)
	lines := 0
	clipped := ""
	for i := 0; i < len(hexdump); i++ {
		if hexdump[i] == '\n' {
			lines++
		}
		if lines == 65 { // a bit more than 1k
			hexdump = hexdump[:i]
			clipped = "<br/>[Clipped]"
			break
		}
	}

	v.printf("%s<pre>%s</pre>%s\n", indent(depth), hexdump, clipped)

	q := url.QueryEscape(path)
	v.printf("%s<a target=\"_blank\" href=\"/binary?path=%s\">Open</a>\n",
		indent(depth), q)

	// TODO handle non-readonly binaries, e.g. via file upload.

	return nil
}

// ----------------------------------------------------------------------------
// Special Types

const timeFormat = "2006-01-02T15:04:05.999Z07:00" // Milliseconds is enough.

func (v *Value) renderTime(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	v.printf("%s", indent(depth))

	t := val.Convert(reflect.TypeOf(time.Time{})).Interface().(time.Time)

	escVal := template.HTMLEscapeString(t.Format(timeFormat))
	if readonly {
		v.printf("<code>%s</code>\n", escVal)
		return nil
	}

	v.printf("<input type=\"text\" name=\"%s\" value=\"%s\" />\n",
		template.HTMLEscapeString(path),
		escVal)
	return nil
}

func (v *Value) renderDuration(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	v.printf("%s", indent(depth))

	dv := val.Convert(reflect.TypeOf(time.Duration(0)))
	d := dv.Interface().(time.Duration)
	escDuration := template.HTMLEscapeString(d.String())

	if readonly {
		v.printf("%s", escDuration)
		return nil
	}

	v.printf("<input type=\"text\" name=\"%s\" value=\"%s\" />\n",
		template.HTMLEscapeString(path),
		escDuration)

	return nil
}

func (v *Value) renderError(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	if val.IsNil() {
		return nil
	}

	v.printf("%s", indent(depth))
	err := val.Interface().(error)
	// TODO: handle non-readonly errors?
	escVal := template.HTMLEscapeString(err.Error())
	v.printf("<strong class=\"error\"><code>%s</code><strong>\n", escVal)
	return nil
}

// ----------------------------------------------------------------------------
// Stuff that gets displayed by its String() method

func (v *Value) renderDisplayString(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	v.printf("%s", indent(depth))

	strMethod := val.MethodByName("String")
	out := strMethod.Call(nil)
	escVal := template.HTMLEscapeString(out[0].String())
	v.printf("<code>%s</code>\n", escVal)
	return nil
}

// ----------------------------------------------------------------------------
// Pointers

func (v *Value) renderPtr(path string, depth int, readonly bool, val reflect.Value) error {
	if val.IsNil() {
		return v.renderNilPtr(path, depth, readonly, val)
	}
	return v.renderNonNilPtr(path, depth, readonly, val)

}

func (v *Value) renderNonNilPtr(path string, depth int, readonly bool, val reflect.Value) error {

	if !readonly {
		op := path + ".__OP__"

		v.printf("%s<button name=\"%s\" value=\"Remove\">-</button>\n",
			indent(depth),
			template.HTMLEscapeString(op),
		)
	}

	return v.render(path, depth, readonly, val.Elem())
}

func (v *Value) renderNilPtr(path string, depth int, readonly bool, val reflect.Value) error {
	if readonly {
		return nil
	}

	op := path + ".__OP__"
	v.printf("%s<button name=\"%s\" value=\"Add\">+</button>\n",
		indent(depth),
		template.HTMLEscapeString(op),
	)
	return nil
}

// ----------------------------------------------------------------------------
// Interface

func (v *Value) renderInterface(path string, depth int, readonly bool, val reflect.Value) error {
	if val.IsNil() {
		return v.renderNilInterface(path, depth, readonly, val)
	}
	return v.renderNonNilInterface(path, depth, readonly, val)

}

func (v *Value) renderNonNilInterface(path string, depth int, readonly bool, val reflect.Value) error {
	if !readonly {
		op := path + ".__OP__"

		v.printf("%s<button name=\"%s\" value=\"Remove\">-</button>\n",
			indent(depth),
			template.HTMLEscapeString(op),
		)
	}

	return v.render(path, depth, readonly, val.Elem())
}

func (v *Value) renderNilInterface(path string, depth int, readonly bool, val reflect.Value) error {
	if readonly {
		return nil
	}

	op := path + ".__TYPE__"
	v.printf("%s<div class=\"implements-buttons\">\n", indent(depth))
	for _, typ := range Implements[val.Type()] {
		if typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
		}
		name, tooltip := v.typeinfo(typ)
		hname := template.HTMLEscapeString(name)

		v.printf("%s<button name=\"%s\" value=\"%s\" style=\"margin-top: 2px; margin-bottom: 2px;\" class=\"tooltip\">%s<span class=\"tooltiptext\"><pre>%s</pre></span></button> &emsp; \n",
			indent(depth+1),
			template.HTMLEscapeString(op),
			hname, hname,
			template.HTMLEscapeString(tooltip),
		)
	}
	v.printf("%s</div>\n", indent(depth))
	return nil
}

// ----------------------------------------------------------------------------
// Slices

func (v *Value) renderSlice(path string, depth int, readonly bool, val reflect.Value) error {
	if val.Type().Elem().Kind() == reflect.Uint8 {
		// Treat byte slices as binary data, not as a slice.
		data := val.Bytes()
		return v.renderBinaryData(path, depth, readonly, data)
	}

	v.renderMessages(path, depth)
	v.printf("%s<table>\n", indent(depth))
	var err error
	for i := 0; i < val.Len(); i++ {
		field := val.Index(i)
		fieldPath := fmt.Sprintf("%s.%d", path, i)

		v.printf("%s<tr id=\"%s\">\n",
			indent(depth+1),
			template.HTMLEscapeString(fieldPath))

		// Index number and controls.
		v.printf("%s<td>%d:</td>\n", indent(depth+2), i)
		if !readonly {
			v.printf("%s<td><button name=\"%s\" value=\"Remove\">-</button></td>\n",
				indent(depth+2),
				template.HTMLEscapeString(fieldPath+".__OP__"),
			)
			if false && i > 0 {
				v.printf("<button>↑</button> ")
			}
		}

		// The field itself.
		v.printf("%s<td>\n", indent(depth+2))
		e := v.render(fieldPath, depth+3, readonly, field)
		if e != nil {
			err = e
		}
		v.printf("%s</td>\n", indent(depth+2))

		v.printf("%s</tr>\n", indent(depth+1))
	}
	v.printf("%s<tr>\n", indent(depth+1))
	if !readonly {
		v.printf("%s<td><button name=\"%s\" value=\"Add\">+</button></td>\n",
			indent(depth+2),
			template.HTMLEscapeString(path+".__OP__"),
		)
	}
	v.printf("%s</tr>\n", indent(depth+1))
	v.printf("%s</table>\n", indent(depth))

	return err
}

// ----------------------------------------------------------------------------
// Structures

// Structs are easy: all fields are fixed, nothing to add or delete.
func (v *Value) renderStruct(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	var err error

	typename, tooltip := v.typeinfo(val)
	v.printf("\n")
	v.printf("%s<fieldset>\n", indent(depth))
	depth++
	v.printf(`%s<legend class="tooltip">%s<span class="tooltiptext"><pre>%s</pre></span></legend>
`,
		indent(depth),
		template.HTMLEscapeString(typename),
		template.HTMLEscapeString(tooltip))

	v.printf("%s<table>\n", indent(depth))
	for i := 0; i < val.NumField(); i++ {
		name, finfo := v.fieldinfo(val, i)
		if unexported(name) || finfo.Omit || unwalkable(val.Field(i)) {
			continue
		}
		fieldpath := path + "." + name

		v.printf("%s<tr id=\"%s\">\n", indent(depth+1), fieldpath)
		tooltip := finfo.Doc
		v.printf(`%s<th class="tooltip">%s:<span class="tooltiptext"><pre>%s</pre></span></th>`+"\n",
			indent(depth+2),
			template.HTMLEscapeString(name),
			template.HTMLEscapeString(tooltip))
		field := val.Field(i)

		v.printf("%s<td>\n", indent(depth+2))
		v.nextfieldinfo = finfo
		e := v.render(fieldpath, depth+3, readonly || finfo.Const, field)
		v.nextfieldinfo = Fieldinfo{}
		if e != nil {
			err = e
		}
		v.printf("%s<td>\n", indent(depth+2))

		v.printf("%s</tr>\n", indent(depth+1))
	}
	v.printf("%s</table>\n", indent(depth))
	depth--

	// <div class="Pass">Pass</div>
	v.printf("%s</fieldset>\n", indent(depth))
	v.printf("\n")

	return err
}

func unexported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return !unicode.IsUpper(r)
}

func unwalkable(val reflect.Value) bool {
	switch val.Kind() {
	case reflect.Invalid, reflect.Array, reflect.Chan, reflect.Func,
		reflect.Complex64, reflect.Complex128, reflect.UnsafePointer:
		return true
	}
	return false
}

// ----------------------------------------------------------------------------
// Maps

// Major problem with maps: Its elements are not addressable and thus
// not setable.

func (v *Value) renderMap(path string, depth int, readonly bool, val reflect.Value) error {
	v.renderMessages(path, depth)
	v.printf("%s<table class=\"map\">\n", indent(depth))
	var err error
	keys := val.MapKeys()

	sortMapKeys(keys)

	for _, k := range keys {
		mv := val.MapIndex(k)
		name := k.String() // BUG: panics if map is indexed by anything else than strings
		elemPath := path + "." + mangleKey(name)
		v.printf("%s<tr id=\"%s\">\n",
			indent(depth+1), template.HTMLEscapeString(elemPath))

		if !readonly {
			v.printf("%s<td><button name=\"%s.__OP__\" value=\"Remove\">-</button></td>\n",
				indent(depth+2), elemPath)
		}
		v.printf("%s<th>%s</th>\n", indent(depth+2),
			template.HTMLEscapeString(name))

		v.printf("%s<td>\n", indent(depth+2))
		e := v.render(elemPath, depth+3, readonly, mv)
		if e != nil {
			err = e
		}
		v.printf("%s</td>\n", indent(depth+2))

		v.printf("%s</tr>\n", indent(depth+1))
	}

	// New entries
	if !readonly {
		v.printf("%s<tr>\n", indent(depth+1))

		v.printf("%s<td colspan=\"2\">\n", indent(depth+2))
		v.printf("%s<input type=\"text\" name=\"%s.__NEW__\" style=\"width: 75px;\"/>\n",
			indent(depth+3), path)
		v.printf("%s</td>\n", indent(depth+2))
		v.printf("%s<td>\n", indent(depth+2))
		v.printf("%s<button name=\"%s.__OP__\" value=\"Add\">+</button>\n",
			indent(depth+3), path)
		v.printf("%s</td>\n", indent(depth+2))

		v.printf("%s</tr>\n", indent(depth+1))
	}

	v.printf("%s</table>\n", indent(depth))

	return err
}

// mangleName takes an arbitrary key of a map and produces a string
// suitable as a HTML form parameter.
func mangleKey(n string) string {
	return n // TODO
}

// demangleKey is the inverse of mangleKey
func demangleKey(n string) string {
	return n // TODO
}

func sortMapKeys(keys []reflect.Value) {
	if len(keys) == 0 {
		return
	}

	if keys[0].Kind() == reflect.String {
		sort.Slice(keys, func(i, j int) bool {
			return keys[i].String() < keys[j].String()
		})
	}

	// TODO at least ints too.
}

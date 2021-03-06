// Copyright 2017 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gui

import (
	"fmt"
	"html/template"
	"net/url"
	"reflect"
	"strconv"
	"time"

	"github.com/vdobler/ht/errorlist"
)

// ValueError is an error from invalid form input, e.g. "foo" to an
// time.Duration field.
type ValueError struct {
	Path string // path of field
	Err  error  // original error
}

func (ve ValueError) Error() string {
	return fmt.Sprintf("field %s: %s", ve.Path, ve.Err.Error())
}

func newValueErrorList(path string, err error) errorlist.List {
	if err == nil {
		return nil
	}
	return errorlist.List{ValueError{Path: path, Err: err}}
}

// addNoticeError is a dead ugly hack to report from walk that an element
// was added.
type addNoticeError string

func (addNoticeError) Error() string { return "-- all is fine --" }

// walk val recursively, producing a copy with updates from form applied.
// Path is the prefix to the current input name.
func walk(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	switch val.Kind() {
	case reflect.Bool:
		return walkBool(form, path, val)
	case reflect.String:
		return walkString(form, path, val)
	case reflect.Int64:
		if isDuration(val) {
			return walkDuration(form, path, val)
		}
		fallthrough
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return walkInt(form, path, val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return walkUint(form, path, val)
	case reflect.Float64, reflect.Float32:
		return walkFloat64(form, path, val)
	case reflect.Struct:
		if isTime(val) {
			return walkTime(form, path, val)
		}
		return walkStruct(form, path, val)
	case reflect.Map:
		return walkMap(form, path, val)
	case reflect.Slice:
		return walkSlice(form, path, val)
	case reflect.Interface:
		return walkInterface(form, path, val)
	case reflect.Ptr:
		return walkPtr(form, path, val)
	}

	fmt.Println("gui: won't walk over", val.Kind().String(), "in", path)

	return reflect.Value{}, nil
}

// ----------------------------------------------------------------------------
// Primitive Types

func walkBool(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	cpy := reflect.New(val.Type()).Elem()

	// Checkbox sends only if checked.
	if _, ok := form[path]; ok {
		delete(form, path)
		cpy.SetBool(true)
	} else {
		cpy.SetBool(false)
	}

	return cpy, nil
}

func walkString(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	cpy := reflect.New(val.Type()).Elem()
	cpy.SetString(val.String())

	if newVals, ok := form[path]; ok {
		delete(form, path)
		if len(newVals) == 1 {
			cpy.SetString(newVals[0])
		} else {
			// Multiple new values stem from checkboxes generated from
			// fields with an Any setting.
			nv := ""
			for _, v := range newVals {
				if v == "" {
					continue
				}
				if nv != "" {
					nv += " "
				}
				nv += v
			}
			cpy.SetString(nv)
		}
	}

	return cpy, nil
}

func walkDuration(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	cpy := reflect.New(val.Type()).Elem()
	cpy.SetInt(val.Int())

	if newVals, ok := form[path]; ok {
		delete(form, path)
		newval, err := time.ParseDuration(newVals[0])
		if err != nil {
			return cpy, newValueErrorList(path, err)
		}
		cpy.SetInt(int64(newval))
	}

	return cpy, nil
}

func walkTime(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	cpy := reflect.New(val.Type()).Elem()
	cpy.Set(val)

	if newVals, ok := form[path]; ok {
		delete(form, path)
		newval, err := time.Parse(timeFormat, newVals[0])
		if err != nil {
			return cpy, newValueErrorList(path, err)
		}
		cpy.Set(reflect.ValueOf(newval))
	}

	return cpy, nil
}

func walkInt(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	cpy := reflect.New(val.Type()).Elem()
	cpy.SetInt(val.Int())

	if newVals, ok := form[path]; ok {
		delete(form, path)
		newVal, err := strconv.ParseInt(newVals[0], 10, 64)
		if err != nil {
			return cpy, newValueErrorList(path, err)
		}
		cpy.SetInt(newVal)
	}

	return cpy, nil
}

func walkUint(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	cpy := reflect.New(val.Type()).Elem()
	cpy.SetUint(val.Uint())

	if newVals, ok := form[path]; ok {
		delete(form, path)
		newVal, err := strconv.ParseInt(newVals[0], 10, 64)
		if err != nil {
			return cpy, newValueErrorList(path, err)
		}
		cpy.SetUint(uint64(newVal)) // BUG mightoverflow
	}

	return cpy, nil
}

func walkFloat64(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	cpy := reflect.New(val.Type()).Elem()
	cpy.SetFloat(val.Float())

	if newVals, ok := form[path]; ok {
		delete(form, path)
		newVal, err := strconv.ParseFloat(newVals[0], 64)
		if err != nil {
			return cpy, newValueErrorList(path, err)
		}
		cpy.SetFloat(newVal)
	}

	return cpy, nil
}

// ----------------------------------------------------------------------------
// Pointers

func walkPtr(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	if val.IsNil() {
		return walkNilPtr(form, path, val)
	}
	return walkNonNilPtr(form, path, val)

}

func walkNonNilPtr(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	op := path + ".__OP__"
	if form.Get(op) == "Remove" {
		delete(form, op)
		delete(form, path)
		return reflect.Zero(val.Type()), nil
	}

	elemcpy, err := walk(form, path, val.Elem())
	cpy := reflect.New(val.Type()).Elem()
	cpy.Set(elemcpy.Addr())
	return cpy, err
}

func walkNilPtr(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	op := path + ".__OP__"
	if form.Get(op) == "Add" {
		delete(form, op)
		delete(form, path)
		elemTyp := val.Type().Elem()
		elem := reflect.New(elemTyp).Elem()
		elem.Set(reflect.Zero(elemTyp))
		err := addNoticeError(path)
		return elem.Addr(), errorlist.List{err}
	}

	return reflect.Zero(val.Type()), nil
}

// ----------------------------------------------------------------------------
// Interfaces

func walkInterface(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	if val.IsNil() {
		return walkNilInterface(form, path, val)
	}
	return walkNonNilInterface(form, path, val)

}

func walkNonNilInterface(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	op := path + ".__OP__"
	if form.Get(op) == "Remove" {
		delete(form, op)
		delete(form, path)
		return reflect.Zero(val.Type()), nil
	}

	elemcpy, err := walk(form, path, val.Elem())
	cpy := reflect.New(val.Type()).Elem()
	cpy.Set(elemcpy)
	return cpy, err
}

func walkNilInterface(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	op := path + ".__TYPE__"
	if name := form.Get(op); name != "" {
		delete(form, op)
		delete(form, path)
		for _, implementor := range Implements[val.Type()] {
			ptr := implementor.Kind() == reflect.Ptr
			implName := implementor.Name()
			if ptr {
				implName = implementor.Elem().Name()
			}
			if implName == name {
				elem := reflect.New(implementor).Elem()
				if ptr {
					elem.Set(reflect.New(implementor.Elem()))
				} else {
					elem.Set(reflect.Zero(implementor))
				}
				err := addNoticeError(path)
				return elem, errorlist.List{err}
			}
		}
		return reflect.Zero(val.Type()),
			newValueErrorList(path, fmt.Errorf("No such type %s", name))
	}

	return reflect.Zero(val.Type()), nil
}

// ----------------------------------------------------------------------------
// Structures

// Structs are easy: all fields are fixed, nothing to add or delete.
func walkStruct(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	var el errorlist.List

	cpy := reflect.New(val.Type()).Elem()
	cpy.Set(reflect.Zero(val.Type())) // TODO: why not cpy.Set(val)?

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		name := val.Type().Field(i).Name
		if unexported(name) || unwalkable(field) {
			continue
		}

		fieldCpy, err := walk(form, path+"."+name, field)
		if err != nil {
			el = el.Append(err)
		}
		cpy.Field(i).Set(fieldCpy)
	}

	return cpy, el
}

// ----------------------------------------------------------------------------
// Slices

func walkSlice(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	cpy := reflect.New(val.Type()).Elem()
	cpy.Set(reflect.MakeSlice(val.Type(), 0, val.Len()))

	var err errorlist.List

	for i := 0; i < val.Len(); i++ {
		elemPath := fmt.Sprintf("%s.%d", path, i)
		op := elemPath + ".__OP__"
		if form.Get(op) == "Remove" {
			delete(form, elemPath)
			delete(form, op)
			continue
		}

		elemCpy, e := walk(form, elemPath, val.Index(i))
		if e != nil {
			err = err.Append(e)
		}
		cpy.Set(reflect.Append(cpy, elemCpy))
	}

	// New elements.
	op := path + ".__OP__"
	if form.Get(op) == "Add" {
		delete(form, op)
		newElem := reflect.Zero(val.Type().Elem())
		cpy.Set(reflect.Append(cpy, newElem))
		ap := fmt.Sprintf("%s.%d", path, cpy.Len()-1)
		err = err.Append(addNoticeError(ap))
	}

	return cpy, err
}

// ----------------------------------------------------------------------------
// Maps

func walkMap(form url.Values, path string, val reflect.Value) (reflect.Value, errorlist.List) {
	cpy := reflect.New(val.Type()).Elem()
	cpy.Set(reflect.MakeMap(val.Type()))

	var err errorlist.List
	for _, k := range val.MapKeys() {
		name := k.String() // BUG: panics if map is indexed by anything else than strings
		elemName := mangleKey(name)
		elemPath := path + "." + elemName

		// Remove key?
		op := elemPath + ".__OP__"
		if form.Get(op) == "Remove" {
			delete(form, elemPath)
			delete(form, op)
			continue
		}

		elemCpy, e := walk(form, elemPath, val.MapIndex(k))
		if e != nil {
			err = err.Append(e)
		}
		cpy.SetMapIndex(k, elemCpy)
	}

	// New key?
	op := path + ".__OP__"
	if form.Get(op) == "Add" {
		delete(form, op)
		if key := form.Get(path + ".__NEW__"); key != "" {
			delete(form, path+".__KEY__")
			newKey := reflect.ValueOf(key) // Bug, works only for string keys
			newElem := reflect.Zero(val.Type().Elem())
			cpy.SetMapIndex(newKey, newElem)
			ap := fmt.Sprintf("%s.%s", path, mangleKey(key))
			err = err.Append(addNoticeError(ap))
		}
	}

	return cpy, err
}

// ----------------------------------------------------------------------------

var displayTemplateStr = `
<doctype html>
<html>
<head>
  <title>Display {{.Filename}}</title>
</head>
<body>
  <h1>{{.Filename}}</h1>
  <form action="/update" method="post>
    {{.Form}}
  </form>
</body>
</html>
`

var displayTemplate *template.Template

func init() {
	displayTemplate = template.New("DISPLAY")
	// SuiteTmpl.Funcs(fm)
	displayTemplate = template.Must(displayTemplate.Parse(displayTemplateStr))
}

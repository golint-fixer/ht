// Copyright 2016 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package populate provides function to populate Go values from
// an untyped interface{} soup.
//
// Arbitrary JSON documents can be unmarshaled into an interface{}
// via encoding/json.Unmarshal. Hjson (human JSON) allows only this
// type of unmarshalling. Both produce a (slightly different) soup
// of interface{}, []interface{} and map[string]interface{}.
//
// Package populate takes such a soup and populates a Go object
// from this soup doing sensible type conversions where appropriate:
//   - Fundamental types (the various ints, bools, strings, floats)
//     work as expected.
//   - Maps work as expected.
//   - Slices work as expected with one syntactical suggar: You can
//     populate a []T from a single instance of T, the resulting slice
//     has length 1 and contains just this T.
//   - time.Durations can be populated from ints or floats (containing
//     the duration in nanoseconds) or from strings like "2.5s" or "45ms"
//     i.e. strings parsable by time.ParseDuration.
//
package populate

import (
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// Populator is the interface a type can implement to provide a custom
// deserialisation.
type Populator interface {
	Populate(src interface{}) error
}

// Strict populates dst from src failing if elements in src cannot be mapped
// to dst.
func Strict(dst, src interface{}) error {
	dv, sv := reflect.ValueOf(dst), reflect.ValueOf(src)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		panic("populate: not a pointer or nil")
	}
	x := reflect.New(dv.Type()).Elem()
	err := recFillWith(x, sv, x.Type().Elem().Name(), true)
	if err != nil {
		return err
	}
	dv.Elem().Set(x.Elem())
	return nil
}

// Lax populates dst from src. Src may contain elements which cannot
// be mapped to dst in which case they are ignored silently.
func Lax(dst, src interface{}) error {
	dv, sv := reflect.ValueOf(dst), reflect.ValueOf(src)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("Not a pointer or nil")
	}
	x := reflect.New(dv.Type()).Elem()
	err := recFillWith(x, sv, x.Type().Elem().Name(), false)
	if err != nil {
		return err
	}
	dv.Elem().Set(x.Elem())
	return nil
}

func setFloat(dst, src reflect.Value, elem string) error {
	f := 0.0

	switch src.Kind() {
	case reflect.Bool:
		if src.Bool() {
			f = 1.0
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f = float64(src.Int())
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		f = float64(src.Uint())
	case reflect.Float64, reflect.Float32:
		f = src.Float()
	case reflect.String:
		s := src.String()
		var err error
		f, err = strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("cannot set %s <%s> to %q", elem, dst.Kind(), s)
		}
	default:
		return fmt.Errorf("cannot set %s <%s> to %v <%s>",
			elem, dst.Kind(), src.Interface(), src.Kind())
	}

	dst.SetFloat(f)
	return nil
}

func setBool(dst, src reflect.Value, elem string) error {
	b := false

	switch src.Kind() {
	case reflect.Bool:
		b = src.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if src.Int() != 0 {
			b = true
		}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if src.Uint() != 0 {
			b = true
		}
	case reflect.Float64, reflect.Float32:
		if src.Float() != 0.0 {
			b = true
		}
	case reflect.String:
		switch src.String() {
		case "true", "TRUE", "1", "yes", "YES":
			b = true
		case "false", "FALSE", "0", "no", "NO":
			b = false
		default:
			return fmt.Errorf("cannot set %s <bool> to %q", elem, src.String())
		}
	default:
		return fmt.Errorf("cannot set %s <%s> to %v <%s>",
			elem, dst.Kind(), src.Interface(), src.Kind())
	}

	dst.SetBool(b)
	return nil
}

func setInt(dst, src reflect.Value, elem string) error {
	i := int64(0)

	switch src.Kind() {
	case reflect.Bool:
		if src.Bool() {
			i = 1
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i = src.Int()
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i = int64(src.Uint())
		if uint64(i) != src.Uint() {
			return fmt.Errorf("cannot set %s <%s> to %d, overflow",
				elem, dst.Kind(), src.Uint())
		}
	case reflect.Float64, reflect.Float32:
		i = int64(src.Float())
	case reflect.String:
		s := src.String()
		var err error
		i, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("cannot set %s <%s> to %q", elem, dst.Kind(), s)
		}
	default:
		return fmt.Errorf("cannot set %s <%s> to %v <%s>",
			elem, dst.Kind(), src.Interface(), src.Kind())
	}

	dst.SetInt(i)
	return nil
}

func setDuration(dst, src reflect.Value, elem string) error {
	switch src.Kind() {
	case reflect.Int64:
		if isDuration(src) {
			dst.Set(src)
			return nil
		}
		fallthrough
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		dst.SetInt(1e9 * src.Int()) // nanoseconds
		return nil
	case reflect.Float64, reflect.Float32:
		dst.SetInt(int64(1e9 * src.Float())) // nanoseconds
	case reflect.String:
		d, err := time.ParseDuration(src.String())
		if err != nil {
			return fmt.Errorf("cannot set %s <Duration> to %q", elem, src.String())
		}
		dst.SetInt(int64(d))
		return nil
	}

	return fmt.Errorf("cannot set %s <Duration> to %v <%s>",
		elem, src.Interface(), src.Kind())
}

func setUint(dst, src reflect.Value, elem string) error {
	panic("not implemented")
}

func setSlice(dst, src reflect.Value, elem string, strict bool) error {
	if !src.IsValid() {
		// Src is a zero Value slice.
		dst.Set(reflect.Zero(dst.Type()))
		return nil
	}

	if src.Kind() == reflect.Slice {
		n := src.Len()
		dst.Set(reflect.MakeSlice(dst.Type(), n, n))
		for i := 0; i < n; i++ {
			err := recFillWith(dst.Index(i), src.Index(i),
				fmt.Sprintf("%s[%d]", elem, i), strict)
			if err != nil {
				return err
			}
		}
		return nil
	}

	// Autogenerated single element slice.
	dst.Set(reflect.MakeSlice(dst.Type(), 1, 1))
	return recFillWith(dst.Index(0), src, fmt.Sprintf("%s[%d]", elem, 0), strict)
}

func setMap(dst, src reflect.Value, elem string, strict bool) error {
	if !src.IsValid() {
		// Src is a zero Value of a map.
		dst.Set(reflect.Zero(dst.Type()))
		return nil
	}

	switch src.Kind() {
	case reflect.Map:
		dst.Set(reflect.MakeMap(dst.Type()))
		for _, key := range src.MapKeys() {
			srcValue := src.MapIndex(key)
			dstValue := reflect.New(dst.Type().Elem()).Elem()
			err := recFillWith(dstValue, srcValue,
				fmt.Sprintf("%s[%v]", elem, key.Interface()), strict)
			if err != nil {
				return err
			}
			// TODO: generate and fill destination key from source key as their types may differ.
			dst.SetMapIndex(key, dstValue)
		}
		return nil
	}

	mt := dst.Type()
	return fmt.Errorf("cannot set %s <map[%s]%s> to %v <%s>",
		elem, mt.Key().Kind(), mt.Elem().Kind(), src.Interface(), src.Kind())
}

func setStruct(dst, src reflect.Value, elem string, strict bool) error {
	switch src.Kind() {
	case reflect.Map:
		for _, key := range src.MapKeys() {
			if key.Kind() != reflect.String {
				return fmt.Errorf("cannot set %s to map with %s keys",
					elem, key.Kind())
			}
			name := key.String()
			srcValue := src.MapIndex(key)
			//field := dst.Type().FieldByName(name)
			field := dst.FieldByName(name)
			if !field.IsValid() {
				if name == "comment" || !strict {
					continue
				}
				return fmt.Errorf("unknown field %s in %s",
					name, elem) // TODO: error is unclear
			}
			err := recFillWith(field, srcValue,
				fmt.Sprintf("%s.%s", elem, name), strict)
			if err != nil {
				return err
			}
		}
		return nil
	}

	return fmt.Errorf("cannot set %s <%s> to %v <%s>",
		elem, dst.Kind(), src.Interface(), src.Kind())
}

func recFillWith(dst, src reflect.Value, elem string, strict bool) error {
	// fmt.Println("recFillWith", elem)
	if src.Kind() == reflect.Interface {
		src = src.Elem()
		// fmt.Printf("Unwrapped interface src to %s\n", src.Kind())
		return recFillWith(dst, src, elem, strict)
	}

	if !dst.CanSet() {
		// This should not happen, or?
		return fmt.Errorf("cannot set element %s (%v)", elem, dst)
	}

	if dst.Kind() != reflect.Ptr && dst.Type().Name() != "" && dst.CanAddr() {
		dstAddr := dst.Addr()
		if p, ok := dstAddr.Interface().(Populator); ok {
			err := p.Populate(src.Interface())
			if err != nil {
				return err
			}
			dst.Set(dstAddr.Elem())
			return nil
		}
	}

	for dst.Kind() == reflect.Ptr {
		elemTyp := dst.Type().Elem()
		dst.Set(reflect.New(elemTyp))
		// TODO: only create new var if src != null
		/*
			err := recFillWith(dst.Elem(), src, fmt.Sprintf("*%s", elem))
			if err != nil {
				return err
			}
		*/
		dst = dst.Elem()
	}

	// fmt.Printf("recFillWith %s (%s) with %s \n", elem, dst.Kind(), src.Kind())

	switch dst.Kind() {
	case reflect.Bool:
		return setBool(dst, src, elem)
	case reflect.Int64:
		if isDuration(dst) {
			return setDuration(dst, src, elem)
		}
		fallthrough
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return setInt(dst, src, elem)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return setUint(dst, src, elem)
	case reflect.Float64, reflect.Float32:
		return setFloat(dst, src, elem)
	case reflect.String:
		dst.SetString(fmt.Sprintf("%v", src.Interface()))
		return nil
	case reflect.Slice:
		return setSlice(dst, src, elem, strict)
	case reflect.Map:
		return setMap(dst, src, elem, strict)
	case reflect.Struct:
		return setStruct(dst, src, elem, strict)
	case reflect.Interface:
		dst.Set(src)
	default:
		return fmt.Errorf("cannot set %s <%s> to <%s>", elem, dst.Kind(), src.Kind())
	}

	return nil
}

func isDuration(v reflect.Value) bool {
	t := v.Type()
	return (t.PkgPath() == "time" && t.Name() == "Duration") ||
		(t.PkgPath() == "github.com/vdobler/ht/ht" && t.Name() == "Duration")
}

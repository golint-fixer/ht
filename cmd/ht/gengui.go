// Copyright 2016 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/format"
	"go/parser"
	"go/token"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/vdobler/ht/gui"
	"github.com/vdobler/ht/ht"
)

var godoc map[string]*doc.Package

func main() {
	godoc = make(map[string]*doc.Package)

	buf := &bytes.Buffer{}
	dumpChecksAndExtr(buf)
	dumpCommonTypes(buf)

	fileBuf := &bytes.Buffer{}
	fileBuf.WriteString(`// generated by go run gengui.go; DO NOT EDIT

package main

import (
    "github.com/vdobler/ht/gui"
`)

	for path := range godoc {
		fmt.Fprintf(fileBuf, "    %q\n", path)
	}
	fileBuf.WriteString(`
)

func init() {
`)
	fileBuf.Write(buf.Bytes())

	fileBuf.WriteString("\n}\n")

	b, err := format.Source(fileBuf.Bytes())
	if err != nil {
		log.Fatalf("cannot format data: %s\n%s", err, fileBuf.Bytes())
	}

	err = ioutil.WriteFile("guidata.go", b, 0644)
	if err != nil {
		log.Fatal(err)
	}
}

func dumpCommonTypes(buf *bytes.Buffer) {
	dumpData(buf, reflect.TypeOf(ht.Test{}))
	dumpData(buf, reflect.TypeOf(ht.Request{}))
	dumpData(buf, reflect.TypeOf(ht.Response{}))
	dumpData(buf, reflect.TypeOf(ht.CheckList{}))
	dumpData(buf, reflect.TypeOf(ht.ExtractorMap{}))
	dumpData(buf, reflect.TypeOf(ht.Extraction{}))
	dumpData(buf, reflect.TypeOf(ht.Execution{}))
	dumpData(buf, reflect.TypeOf(ht.Cookie{}))
	dumpData(buf, reflect.TypeOf(ht.Condition{}))
	dumpData(buf, reflect.TypeOf(ht.Browser{}))
	dumpData(buf, reflect.TypeOf(url.Values{}))
	dumpData(buf, reflect.TypeOf(http.Header{}))
	dumpData(buf, reflect.TypeOf(http.Response{}))
}

func dumpChecksAndExtr(buf *bytes.Buffer) {
	names := make([]string, 0, len(ht.CheckRegistry))
	for name := range ht.CheckRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		typ := ht.CheckRegistry[name]
		if typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
		}
		dumpData(buf, typ)
	}

	names = make([]string, 0, len(ht.ExtractorRegistry))
	for name := range ht.ExtractorRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		typ := ht.ExtractorRegistry[name]
		if typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
		}
		dumpData(buf, typ)
	}
}

func dumpData(buf *bytes.Buffer, t reflect.Type) {
	importPath, symbol := t.PkgPath(), t.Name()
	ti := infoFor(importPath, symbol)

	pkg := importPath[strings.LastIndex(importPath, "/")+1:]
	typeLit := fmt.Sprintf("%s.%s{}", pkg, symbol)
	infoLit := fmt.Sprintf(`gui.Typeinfo{
    Doc: %q,
    Field: map[string]gui.Fieldinfo{`, ti.Doc)
	warnIfTooBroad(symbol, ti.Doc)

	names := make([]string, 0, len(ti.Field))
	for name := range ti.Field {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, field := range names {
		infoLit += fmt.Sprintf(`
        %q: gui.Fieldinfo{
            Doc: %q,
        },`, field, ti.Field[field].Doc)
		warnIfTooBroad(symbol+"."+field, ti.Field[field].Doc)

	}
	infoLit += "}}"

	fmt.Fprintf(buf, "gui.RegisterType(%s, %s)\n\n", typeLit, infoLit)
}

func warnIfTooBroad(symbol, doc string) {
	for _, line := range strings.Split(doc, "\n") {
		if len(line) > 80 {
			fmt.Printf("Type Doc for %s too long:\n  %s\n",
				symbol, line)
		}
	}

}

func infoFor(importPath, symbol string) gui.Typeinfo {
	pkgDoc, ok := godoc[importPath]
	if !ok {
		pkg, err := build.Import(importPath, "", build.ImportComment)
		if err != nil {
			log.Fatal(err)
		}

		fs := token.NewFileSet()
		include := func(info os.FileInfo) bool {
			for _, name := range pkg.GoFiles {
				if name == info.Name() {
					return true
				}
			}
			return false
		}
		pkgs, err := parser.ParseDir(fs, pkg.Dir, include, parser.ParseComments)
		if err != nil {
			log.Fatal(err)
		}

		pkgAst := pkgs[pkg.Name]
		pkgDoc = doc.New(pkgAst, pkg.ImportPath, 0)
		godoc[importPath] = pkgDoc
	}

	return typeinfo(pkgDoc, symbol)
}

// typeDoc returns the Godoc for the type name and its fields in the given package.
func typeinfo(pkgDoc *doc.Package, typeName string) gui.Typeinfo {
	tinfo := gui.Typeinfo{}

	// find type
	var typ *doc.Type
	for i := range pkgDoc.Types {
		if typeName == pkgDoc.Types[i].Name {
			typ = pkgDoc.Types[i]
			break
		}
	}
	if typ == nil {
		log.Fatal("no such type", typeName)
	}

	buf := &bytes.Buffer{}
	doc.ToText(buf, typ.Doc, "", "    ", 80)
	tinfo.Doc = buf.String()

	spec := typeSpec(typ.Decl, typ.Name)
	structType, ok := spec.Type.(*ast.StructType)
	if !ok {
		if ident, ok := spec.Type.(*ast.Ident); ok {
			ti := typeinfo(pkgDoc, ident.Name)
			tinfo.Field = ti.Field
		} else {
			fmt.Printf("%s is neither a struct nor a named type but a %T %#v\n",
				typeName, spec.Type, spec.Type)
		}
		return tinfo
	}

	tinfo.Field = make(map[string]gui.Fieldinfo)
	for _, field := range structType.Fields.List {
		fieldNames := []string{}

		if len(field.Names) == 0 {
			// Anonymous field
			ident, ok := field.Type.(*ast.Ident)
			if !ok {
				log.Fatalf("Strange anonymous field: %#v\n", field.Type)
			}
			fieldNames = []string{ident.Name}
		} else {
			for i := range field.Names {
				fieldNames = append(fieldNames, field.Names[i].Name)
			}
		}

		for _, fieldName := range fieldNames {
			buf.Reset()
			if field.Doc != nil {
				doc.ToText(buf, field.Doc.Text(), "", "    ", 80)
			}
			if field.Comment != nil {
				buf.WriteString(field.Comment.List[0].Text)
			}

			tinfo.Field[fieldName] = gui.Fieldinfo{
				Doc: buf.String(),
			}
		}
	}

	return tinfo
}

func typeSpec(decl *ast.GenDecl, symbol string) *ast.TypeSpec {
	for _, spec := range decl.Specs {
		typeSpec := spec.(*ast.TypeSpec)
		if symbol == typeSpec.Name.Name {
			return typeSpec
		}
	}
	return nil
}

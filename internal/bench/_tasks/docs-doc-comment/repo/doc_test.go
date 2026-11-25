package p

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestDocComments(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "shapes.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, doc *ast.CommentGroup) {
		if !ast.IsExported(name) {
			return
		}
		if doc == nil || !strings.HasPrefix(doc.Text(), name+" ") {
			t.Errorf("%s has no doc comment starting with its name", name)
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			check(d.Name.Name, d.Doc)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok {
					check(ts.Name.Name, d.Doc)
				}
			}
		}
	}
}

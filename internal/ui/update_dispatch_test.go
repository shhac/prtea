package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestUpdateHandlesEveryMessageType guards the flat dispatch: every exported
// ...Msg type declared in messages.go must appear as a case in App.Update,
// so a newly added message cannot be silently dropped.
func TestUpdateHandlesEveryMessageType(t *testing.T) {
	fset := token.NewFileSet()

	msgFile, err := parser.ParseFile(fset, "messages.go", nil, 0)
	if err != nil {
		t.Fatalf("parse messages.go: %v", err)
	}
	var msgTypes []string
	for _, decl := range msgFile.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts := spec.(*ast.TypeSpec)
			name := ts.Name.Name
			if strings.HasSuffix(name, "Msg") && ast.IsExported(name) {
				msgTypes = append(msgTypes, name)
			}
		}
	}
	if len(msgTypes) == 0 {
		t.Fatal("no message types found — parser broken?")
	}

	appFile, err := parser.ParseFile(fset, "app.go", nil, 0)
	if err != nil {
		t.Fatalf("parse app.go: %v", err)
	}
	handled := make(map[string]bool)
	for _, decl := range appFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Update" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range cc.List {
				if ident, ok := expr.(*ast.Ident); ok {
					handled[ident.Name] = true
				}
			}
			return true
		})
	}
	if len(handled) == 0 {
		t.Fatal("no case clauses found in App.Update")
	}

	for _, name := range msgTypes {
		if !handled[name] {
			t.Errorf("message type %s has no case in App.Update — it would be silently dropped", name)
		}
	}
}

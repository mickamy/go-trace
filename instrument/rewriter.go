// Package instrument rewrites Go source files to insert tracing calls.
package instrument

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
)

const tracerImportPath = `"github.com/mickamy/go-trace/runtime"`

// Rewrite parses the given Go source and inserts tracer.Enter/finish calls
// into exported methods. It returns the rewritten source.
func Rewrite(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}

	if !rewriteFuncs(file) {
		return src, nil
	}

	addImport(file)

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return nil, fmt.Errorf("format rewritten source: %w", err)
	}
	return buf.Bytes(), nil
}

// rewriteFuncs inserts tracing preamble into exported functions/methods
// that accept a context.Context as the first parameter.
// Returns true if any function was rewritten.
func rewriteFuncs(file *ast.File) bool {
	modified := false

	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if !fn.Name.IsExported() {
			return false
		}
		if fn.Body == nil {
			return false
		}
		ctxParam := contextParam(fn)
		if ctxParam == "" {
			return false
		}
		if hasTracerCall(fn) {
			return false
		}

		name := funcName(fn)
		preamble := buildPreamble(ctxParam, name)
		fn.Body.List = append(preamble, fn.Body.List...)
		modified = true
		return false
	})

	return modified
}

// contextParam returns the name of the first parameter if its type is
// context.Context, or empty string otherwise.
func contextParam(fn *ast.FuncDecl) string {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return ""
	}

	first := fn.Type.Params.List[0]
	typeName := exprString(first.Type)
	if typeName != "context.Context" {
		return ""
	}
	if len(first.Names) == 0 {
		return ""
	}
	return first.Names[0].Name
}

// funcName returns "Receiver.Method" or "Function".
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recv := fn.Recv.List[0]
		return exprString(recv.Type) + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// exprString returns a simple string representation of a type expression.
func exprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return exprString(t.X)
	default:
		return ""
	}
}

// hasTracerCall checks if the function body already contains __gotraceTracer.Enter.
func hasTracerCall(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok {
			if ident.Name == "__gotraceTracer" && sel.Sel.Name == "Enter" {
				found = true
			}
		}
		return !found
	})
	return found
}

// buildPreamble creates the AST statements for:
//
//	ctx, __gotraceFinish := __gotraceTracer.Enter(ctx, "Name", gotraceruntime.SpanKindFunction)
//	defer __gotraceFinish(nil)
func buildPreamble(ctxParam, name string) []ast.Stmt {
	assign := &ast.AssignStmt{
		Lhs: []ast.Expr{
			ast.NewIdent(ctxParam),
			ast.NewIdent("__gotraceFinish"),
		},
		Tok: token.DEFINE,
		Rhs: []ast.Expr{
			&ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   ast.NewIdent("__gotraceTracer"),
					Sel: ast.NewIdent("Enter"),
				},
				Args: []ast.Expr{
					ast.NewIdent(ctxParam),
					&ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", name)},
					&ast.SelectorExpr{
						X:   ast.NewIdent("gotraceruntime"),
						Sel: ast.NewIdent("SpanKindFunction"),
					},
				},
			},
		},
	}

	deferStmt := &ast.DeferStmt{
		Call: &ast.CallExpr{
			Fun: ast.NewIdent("__gotraceFinish"),
			Args: []ast.Expr{
				ast.NewIdent("nil"),
			},
		},
	}

	return []ast.Stmt{assign, deferStmt}
}

// addImport ensures the runtime import is present.
func addImport(file *ast.File) {
	for _, imp := range file.Imports {
		if imp.Path.Value == tracerImportPath {
			return
		}
	}

	newImport := &ast.ImportSpec{
		Name: ast.NewIdent("gotraceruntime"),
		Path: &ast.BasicLit{
			Kind:  token.STRING,
			Value: tracerImportPath,
		},
	}

	// Find existing import declaration or create one.
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		genDecl.Specs = append(genDecl.Specs, newImport)
		return
	}

	file.Decls = append([]ast.Decl{
		&ast.GenDecl{
			Tok:   token.IMPORT,
			Specs: []ast.Spec{newImport},
		},
	}, file.Decls...)
}

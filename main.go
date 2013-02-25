package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io/ioutil"
	"os"
	"path/filepath"
)

func Err(s string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error:"+s+"\n", args...)
}

func main() {
	flag.Parse()
	pkgName := flag.Arg(0)
	if pkgName == "" {
		Err("you must specify a package")
		flag.Usage()
		os.Exit(1)
	}

	pkg, err := build.Import(pkgName, ".", 0)
	if err != nil {
		Err("could not import %s: %s", pkgName, err)
		os.Exit(1)
	}

	files := make([]string, len(pkg.GoFiles))
	for i, fileName := range pkg.GoFiles {
		files[i] = filepath.Join(pkg.Dir, fileName)
	}

	if err := checkFiles(files); err != nil {
		Err("failed to check package: %s", err)
		os.Exit(1)
	}
}

type file struct {
	fset  *token.FileSet
	name  string
	ast   *ast.File
	lines [][]byte
}

func parseFile(fset *token.FileSet, fileName string) (f file, err error) {
	rd, err := os.Open(fileName)
	if err != nil {
		return f, err
	}
	defer rd.Close()

	data, err := ioutil.ReadAll(rd)
	if err != nil {
		return f, err
	}

	astFile, err := parser.ParseFile(fset, fileName, bytes.NewReader(data), parser.ParseComments)
	if err != nil {
		return f, fmt.Errorf("could not parse: %s", err)
	}

	f = file{fset: fset, name: fileName, ast: astFile, lines: bytes.Split([]byte("\n"), data)}
	return f, nil
}

func typeCheck(fset *token.FileSet, astFiles []*ast.File) (map[*ast.CallExpr]types.Type, error) {
	callTypes := make(map[*ast.CallExpr]types.Type)

	exprFn := func(x ast.Expr, typ types.Type, val interface{}) {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return
		}
		callTypes[call] = typ
	}
	context := types.Context{
		Expr: exprFn,
	}
	_, err := context.Check(fset, astFiles)
	return callTypes, err
}

func checkFiles(fileNames []string) error {
	fset := token.NewFileSet()

	astFiles := make([]*ast.File, len(fileNames))

	files := make([]file, len(fileNames))

	for i, fileName := range fileNames {
		f, err := parseFile(fset, fileName)
		if err != nil {
			return fmt.Errorf("could not parse %s: %s", fileName, err)
		}
		files[i] = f
		astFiles[i] = f.ast
	}

	callTypes, err := typeCheck(fset, astFiles)
	if err != nil {
		return fmt.Errorf("could not type check: %s", err)
	}

	visitor := func(node ast.Node) {
		n, ok := node.(*ast.ExprStmt)
		if !ok {
			return
		}

		// Check for a call expression
		call, ok := n.X.(*ast.CallExpr)
		if !ok {
			return
		}

		var fun *ast.Ident
		switch exp := call.Fun.(type) {
		case (*ast.Ident):
			fun = exp
		case (*ast.SelectorExpr):
			fun = exp.Sel
		default:
			fmt.Fprintf(os.Stderr, "unknown call: %T %+v\n", exp, exp)
			return
		}

		// Get the types
		callType := callTypes[call]

		unchecked := false

		switch t := callType.(type) {
		case *types.NamedType:
			// Single return
			if isErrorType(t.Obj) {
				unchecked = true
			}
		case *types.Result:
			// Multiple returns
			for _, v := range t.Values {
				nt, ok := v.Type.(*types.NamedType)
				if !ok {
					continue
				}
				if isErrorType(nt.Obj) {
					unchecked = true
					break
				}
			}
		}

		if unchecked {
			fmt.Fprintf(os.Stdout, "%s\n", fset.Position(fun.NamePos))
		}
	}

	for _, astFile := range astFiles {
		ast.Walk(visitorFunc(visitor), astFile)
	}
	//	ast.Fprint(os.Stderr, fset, astFile, nil)

	return nil
}

type obj interface {
	GetPkg() *types.Package
	GetName() string
}

func isErrorType(v obj) bool {
	return v.GetPkg() == nil && v.GetName() == "error"
}

type visitorFunc func(node ast.Node)

func (v visitorFunc) Visit(node ast.Node) ast.Visitor {
	v(node)
	return v
}
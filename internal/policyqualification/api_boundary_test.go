package policyqualification

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestGateHasNoCallerSettableCompletionField(t *testing.T) {
	typeOfGate := reflect.TypeOf(Gate{})
	for index := 0; index < typeOfGate.NumField(); index++ {
		if typeOfGate.Field(index).IsExported() {
			t.Fatalf("Gate exposes caller-settable field %s", typeOfGate.Field(index).Name)
		}
	}
}

func TestOnlyDurableReplayCanReturnGate(t *testing.T) {
	files := parseProductionFiles(t)
	for name, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Type.Results == nil {
				continue
			}
			for _, result := range function.Type.Results.List {
				if returnsGate(result.Type) && function.Name.Name != "Replay" {
					t.Fatalf("%s exposes alternate Gate constructor %s", name, function.Name.Name)
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok || !returnsGate(literal.Type) {
				return true
			}
			position := filePosition(file, literal.Pos())
			if position != "Replay" && len(literal.Elts) != 0 {
				t.Fatalf("%s constructs a non-zero Gate outside Replay", name)
			}
			return true
		})
	}
}

func filePosition(file *ast.File, position token.Pos) string {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Pos() <= position && position <= function.End() {
			return function.Name.Name
		}
	}
	return ""
}

func returnsGate(expression ast.Expr) bool {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name == "Gate"
	case *ast.StarExpr:
		identifier, ok := typed.X.(*ast.Ident)
		return ok && identifier.Name == "Gate"
	default:
		return false
	}
}

func TestQualificationRecorderHasNoCloudOrMutationDependency(t *testing.T) {
	banned := []string{
		"/internal/cloudruntime", "/internal/cloudingest", "/internal/database",
		"/internal/ipc", "/internal/operator", "/internal/policycontrol",
		"/internal/policystore", "/internal/resumeexecutor", "net", "os/exec",
	}
	for name, file := range parseProductionFiles(t) {
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range banned {
				if value == forbidden || strings.Contains(value, forbidden) {
					t.Fatalf("%s imports forbidden authority %q", name, value)
				}
			}
		}
	}
}

func parseProductionFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]*ast.File)
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = parsed
	}
	return files
}

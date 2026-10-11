// Command sqlpolicy enforces the GORM query policy: typed conditions only,
// no raw SQL outside the allowlist. It walks the root module with go/ast
// and fails on any banned call. Schema tag strings are declaration, not
// queries, so tags are never inspected.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// allowlisted files own the finite raw-SQL exceptions.
var allowlistedFiles = map[string]bool{
	"internal/db/maintenance.go": true,
	"internal/db/config.go":      true,
}

// unconditional bans fire on any call of the method.
var bannedCalls = map[string]bool{
	"Raw":           true,
	"Exec":          true,
	"Save":          true,
	"Pluck":         true,
	"Distinct":      true,
	"Group":         true,
	"Having":        true,
	"Joins":         true,
	"InnerJoins":    true,
	"Table":         true,
	"FirstOrCreate": true,
	"FirstOrInit":   true,
}

// stringConditionBans fire only when the first argument is a string literal.
var stringConditionBans = map[string]bool{
	"Where":  true,
	"Not":    true,
	"Or":     true,
	"Order":  true,
	"Select": true,
}

var skipDirectories = map[string]bool{
	".git":         true,
	".workspace":   true,
	"node_modules": true,
	"build":        true,
	"scripts":      true,
	"web":          true,
}

func main() {
	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}
	var violations []string
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if skipDirectories[entry.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if allowlistedFiles[relative] {
			return nil
		}
		found, err := inspectFile(path, relative)
		if err != nil {
			return err
		}
		violations = append(violations, found...)
		return nil
	})
	if walkErr != nil {
		shared.Failf("walk tree: %v", walkErr)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		fmt.Fprintf(os.Stderr, "sql-policy violations:\n")
		for _, violation := range violations {
			fmt.Fprintf(os.Stderr, "  %s\n", violation)
		}
		shared.Failf("sql-policy: %d violations (typed clause conditions only; raw SQL lives in internal/db/maintenance.go)", len(violations))
	}
	shared.OKf("sql-policy clean")
}

func inspectFile(path, relative string) ([]string, error) {
	fileSet := token.NewFileSet()
	full, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", relative, err)
	}
	usesGorm, usesClause := fileImports(full)
	var violations []string
	ast.Inspect(full, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CallExpr:
			violation := inspectCall(fileSet, relative, typed, usesGorm)
			if violation != "" {
				violations = append(violations, violation)
			}
		case *ast.CompositeLit:
			if !usesClause {
				return true
			}
			violation := inspectComposite(fileSet, relative, typed)
			if violation != "" {
				violations = append(violations, violation)
			}
		}
		return true
	})
	return violations, nil
}

func fileImports(parsed *ast.File) (usesGorm bool, usesClause bool) {
	for _, declaration := range parsed.Imports {
		importPath := strings.Trim(declaration.Path.Value, "\"")
		if importPath == "gorm.io/gorm" || strings.HasPrefix(importPath, "gorm.io/gorm/") {
			usesGorm = true
		}
		if importPath == "gorm.io/gorm/clause" {
			usesClause = true
		}
	}
	return usesGorm, usesClause
}

func inspectCall(fileSet *token.FileSet, relative string, call *ast.CallExpr, usesGorm bool) string {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	name := selector.Sel.Name
	if name == "Expr" {
		if identifier, ok := selector.X.(*ast.Ident); ok && identifier.Name == "gorm" {
			return fmt.Sprintf("%s:%d: banned call gorm.Expr(", relative, lineOf(fileSet, call))
		}
		return ""
	}
	if !usesGorm {
		return ""
	}
	if bannedCalls[name] {
		return fmt.Sprintf("%s:%d: banned call .%s(", relative, lineOf(fileSet, call), name)
	}
	if !stringConditionBans[name] {
		return ""
	}
	if len(call.Args) == 0 {
		return ""
	}
	if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
		return fmt.Sprintf("%s:%d: banned string condition .%s(", relative, lineOf(fileSet, call), name)
	}
	return ""
}

func inspectComposite(fileSet *token.FileSet, relative string, literal *ast.CompositeLit) string {
	selector, ok := literal.Type.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	used, ok := selector.X.(*ast.Ident)
	if !ok || used.Name != "clause" {
		return ""
	}
	if selector.Sel.Name == "Expr" || selector.Sel.Name == "NamedExpr" {
		return fmt.Sprintf("%s:%d: banned literal clause.%s{", relative, lineOf(fileSet, literal), selector.Sel.Name)
	}
	return ""
}

func lineOf(fileSet *token.FileSet, node ast.Node) int {
	return fileSet.Position(node.Pos()).Line
}

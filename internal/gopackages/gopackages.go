// Package gopackages loads a Go module into the mixedmodellint IR with
// golang.org/x/tools/go/packages, go/types and go/ssa, following the approach
// of golang.org/x/vuln's vulncheck (as mirrored in invuln): load syntax and
// type information, build SSA, and match calls by the callee's package,
// receiver type and name.
//
// It produces two things:
//   - entities: persisted structs, read from go/types, so embedded structs and
//     referenced types resolve across packages;
//   - snippets: SQL passed to database/sql, pgx, sqlx and gorm calls, with
//     constant concatenation and branches folded by SSA, for the query corpus.
//
// Load is the only function that performs I/O: it runs `go list` through
// go/packages. Everything after loading is a pure function of the loaded
// packages.
package gopackages

import (
	"context"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// loadMode is packages.LoadSyntax plus module information: syntax and types
// for the module's own packages, and types from export data for dependencies,
// which is all SSA needs to build the module's own functions.
const loadMode = packages.LoadSyntax | packages.NeedModule

// Snippet is SQL found in Go code. Pos is the call site; every finding about
// the SQL points there, because offsets inside a folded string do not map back
// to one place in the Go source. Strict is true when the callee is a known SQL
// API (database/sql, pgx, sqlx, gorm), so SQL that does not parse is a problem
// worth reporting; it is false for a method that is only named like one (an
// sqlc DBTX interface), whose argument may not be SQL at all.
//
// Args holds, for each bind parameter $n at index n-1, the origins of the Go
// value passed for it: a field copied from a persisted struct, a time
// truncated before the write (rule_plan.md §7.4-D, §9.8-B).
type Snippet struct {
	SQL    string
	Pos    mml.Position
	Strict bool
	Args   [][]mml.Origin
}

// Result is what Load found in one module.
type Result struct {
	Source   mml.Source
	Snippets []Snippet
}

// Load loads every package of the Go module containing dir and returns its
// entities and SQL snippets. Positions are relative to root, the directory
// being linted.
//
// Requires: dir is root or below it; the go command is on PATH.
// Ensures:  a package with errors still contributes what type-checked, and
// each error is a Problem at its position; only a failure to run go list at
// all is returned as an error.
func Load(ctx context.Context, root, dir string) (Result, error) {
	const op = "gopackages.Load"
	cfg := &packages.Config{Context: ctx, Dir: dir, Mode: loadMode}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return Result{}, &mml.Error{Op: op, Err: err}
	}
	rel := relativizer(root)
	var res Result
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		res.Source.Problems = append(res.Source.Problems, packageProblems(p, root)...)
	})
	if len(pkgs) == 0 {
		return res, nil
	}
	var persisted map[*types.TypeName]bool
	res.Source.Entities, persisted = entities(pkgs, rel)

	prog, ssaPkgs := ssautil.Packages(pkgs, ssa.BuilderMode(0))
	prog.Build()
	fns := ownFunctions(prog, ssaPkgs)
	tr := newTracer(prog, fns, persisted, rel)
	res.Snippets = snippets(fns, tr)
	res.Source.Queries = tr.modelWrites(fns)
	return res, nil
}

// packageProblems turns p's load errors into Problems. When p has parse or
// type errors, its go list errors are dropped: go list compiles the package
// for export data and reports the same failure again, without a position.
func packageProblems(p *packages.Package, root string) []mml.Problem {
	precise := slices.ContainsFunc(p.Errors, func(e packages.Error) bool {
		return e.Kind == packages.ParseError || e.Kind == packages.TypeError
	})
	var out []mml.Problem
	for _, e := range p.Errors {
		if precise && e.Kind == packages.ListError {
			continue
		}
		out = append(out, mml.Problem{
			Message: "Go package " + p.PkgPath + " has errors: " + e.Msg,
			Pos:     errorPos(e.Pos, root),
		})
	}
	return out
}

// relativizer returns a function that turns a token position into an IR
// position whose file is relative to root.
func relativizer(root string) func(token.Position) mml.Position {
	return func(p token.Position) mml.Position {
		file := p.Filename
		if r, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(r, "..") {
			file = filepath.ToSlash(r)
		}
		return mml.Position{File: file, Line: p.Line, Col: p.Column}
	}
}

// errorPos parses a go/packages error position, "file:line:col",
// "file:line" or "file", into an IR position relative to root. Numbers are
// taken from the right, so a Windows drive letter's colon stays in the file.
func errorPos(pos, root string) mml.Position {
	if pos == "" || pos == "-" {
		return mml.Position{}
	}
	p := token.Position{Filename: pos}
	var nums []int
	for range 2 {
		i := strings.LastIndexByte(p.Filename, ':')
		if i < 0 {
			break
		}
		n, err := strconv.Atoi(p.Filename[i+1:])
		if err != nil {
			break
		}
		nums = append(nums, n)
		p.Filename = p.Filename[:i]
	}
	switch len(nums) {
	case 2:
		p.Line, p.Column = nums[1], nums[0]
	case 1:
		p.Line = nums[0]
	}
	return relativizer(root)(p)
}

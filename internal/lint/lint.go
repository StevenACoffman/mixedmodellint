// Package lint is the composition shell: it walks a directory, hands each
// file's bytes to the adapter for its kind, loads each Go module through
// go/packages, and calls the pure core. It is the only package that imports
// more than one adapter (summary_rules §1).
package lint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/StevenACoffman/mixedmodellint/internal/gopackages"
	"github.com/StevenACoffman/mixedmodellint/internal/jsonschema"
	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
	"github.com/StevenACoffman/mixedmodellint/internal/postgres"
	"github.com/StevenACoffman/mixedmodellint/internal/sidecar"
)

// SidecarName is the sidecar catalog's file name, read from the root of the
// directory being linted.
const SidecarName = "mml.yaml"

// Options configures a run. The zero value lints with no baseline.
type Options struct {
	// Baseline, when set, is read as a baseline file (see mml.WriteBaseline).
	Baseline io.Reader
}

// inputs is every file the walk collected, by adapter, and the Go modules
// it found.
type inputs struct {
	sql     []postgres.File
	schemas []jsonschema.File
	sidecar []byte
	// modules are the directories, relative to the root, that hold a go.mod.
	modules []string
	// hasGo is true when any Go source was seen, so a tree of Go files with no
	// go.mod of its own is still loaded (from a module above the root).
	hasGo bool
}

// Run lints every SQL, Go and JSON Schema file under dir and returns the
// report. Paths in findings are relative to dir.
//
// Requires: dir is a directory; the go command is on PATH when dir holds Go.
// Ensures:  only I/O failures, and failing to run go list, are errors; a file
// that does not parse is a finding in the report.
func Run(ctx context.Context, dir string, opts Options) (mml.Report, error) {
	const op = "lint.Run"
	root, err := filepath.Abs(dir)
	if err != nil {
		return mml.Report{}, &mml.Error{Op: op, Err: err}
	}
	in, err := collect(os.DirFS(root))
	if err != nil {
		return mml.Report{}, &mml.Error{Op: op, Err: err}
	}
	goSrc, snippets, err := loadGo(ctx, root, in.goModules())
	if err != nil {
		return mml.Report{}, &mml.Error{Op: op, Err: err}
	}
	input := mml.Input{Sources: []mml.Source{
		postgres.Load(in.sql, snippets),
		goSrc,
		jsonschema.Load(in.schemas),
	}}
	if in.sidecar != nil {
		sc, problems := sidecar.Parse(SidecarName, in.sidecar)
		input.Sidecar = sc
		input.Sources = append(input.Sources, mml.Source{Problems: problems})
	}
	if opts.Baseline != nil {
		input.Baseline, err = mml.ReadBaseline(opts.Baseline)
		if err != nil {
			return mml.Report{}, &mml.Error{Op: op, Err: err}
		}
	}
	return mml.Analyze(&input), nil
}

// goModules returns the module directories to load: every go.mod the walk
// found, or the root itself when it holds Go files but no go.mod, so Go below
// a module root above the linted directory is still read.
func (in *inputs) goModules() []string {
	if len(in.modules) == 0 && in.hasGo {
		return []string{"."}
	}
	return in.modules
}

// loadGo loads each Go module in modules, relative to root, and returns their
// entities and the SQL they pass to SQL APIs, ready for the postgres adapter.
func loadGo(
	ctx context.Context,
	root string,
	modules []string,
) (mml.Source, []postgres.Snippet, error) {
	var src mml.Source
	var snippets []postgres.Snippet
	for _, m := range modules {
		res, err := gopackages.Load(ctx, root, filepath.Join(root, m))
		if err != nil {
			return mml.Source{}, nil, fmt.Errorf("load Go module %s: %w", m, err)
		}
		src.Entities = append(src.Entities, res.Source.Entities...)
		src.Problems = append(src.Problems, res.Source.Problems...)
		for _, sn := range res.Snippets {
			snippets = append(
				snippets,
				postgres.Snippet{SQL: sn.SQL, Pos: sn.Pos, Strict: sn.Strict, Args: sn.Args},
			)
		}
	}
	return src, snippets, nil
}

// collect walks fsys in lexical order, which is migration order for numbered
// SQL files, and reads each file an adapter wants.
func collect(fsys fs.FS) (inputs, error) {
	var in inputs
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", p, err)
		}
		if d.IsDir() {
			if p != "." && skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		return in.add(fsys, p)
	})
	if err != nil {
		return inputs{}, fmt.Errorf("collect sources: %w", err)
	}
	return in, nil
}

func (in *inputs) add(fsys fs.FS, p string) error {
	kind := fileKind(p)
	switch kind {
	case "":
		return nil
	case "gomod":
		in.modules = append(in.modules, path.Dir(p))
		return nil
	case "go":
		in.hasGo = true
		return nil
	}
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	switch kind {
	case "sql":
		in.sql = append(in.sql, postgres.File{Path: p, Src: src})
	case "json":
		if jsonschema.IsSchema(p, src) {
			in.schemas = append(in.schemas, jsonschema.File{Path: p, Src: src})
		}
	case "sidecar":
		in.sidecar = src
	default:
		return errors.New("unknown file kind " + kind)
	}
	return nil
}

func fileKind(p string) string {
	switch {
	case p == SidecarName:
		return "sidecar"
	case path.Base(p) == "go.mod":
		return "gomod"
	case strings.HasSuffix(p, ".sql"):
		return "sql"
	case strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go"):
		return "go"
	case path.Ext(p) == ".json":
		return "json"
	default:
		return ""
	}
}

// skipDir reports directories that hold no model sources of the project
// itself: VCS metadata, dependencies, build output, and test fixtures.
func skipDir(name string) bool {
	switch name {
	case "node_modules", "vendor", "testdata", "bin", "dist", "target":
		return true
	default:
		return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
	}
}

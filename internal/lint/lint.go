// Package lint is the composition shell: it walks a directory, hands each
// file's bytes to the adapter for its kind, and calls the pure core. It is the
// only package that imports more than one adapter (summary_rules §1).
package lint

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/StevenACoffman/mixedmodellint/internal/goast"
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

// inputs is every file the walk collected, by adapter.
type inputs struct {
	sql     []postgres.File
	gosrc   []goast.File
	schemas []jsonschema.File
	sidecar []byte
}

// Run lints every SQL, Go and JSON Schema file under fsys and returns the
// report. Paths in findings are relative to fsys's root.
//
// Requires: fsys is the directory to lint, typically os.DirFS(dir).
// Ensures:  only I/O failures are errors; a file that does not parse is a
// finding in the report.
func Run(fsys fs.FS, opts Options) (mml.Report, error) {
	const op = "lint.Run"
	in, err := collect(fsys)
	if err != nil {
		return mml.Report{}, &mml.Error{Op: op, Err: err}
	}
	input := mml.Input{Sources: []mml.Source{
		postgres.Load(in.sql),
		goast.Load(in.gosrc),
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
	return mml.Analyze(input), nil
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
	if kind == "" {
		return nil
	}
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	switch kind {
	case "sql":
		in.sql = append(in.sql, postgres.File{Path: p, Src: src})
	case "go":
		in.gosrc = append(in.gosrc, goast.File{Path: p, Src: src})
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

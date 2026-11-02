// Package lint is the composition shell: it walks a directory, hands each
// file's bytes to the adapter for its kind, loads each Go module through
// go/packages, and calls the pure core. It is the only package that imports
// more than one adapter (summary_rules §1).
package lint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/StevenACoffman/mixedmodellint/internal/git"
	"github.com/StevenACoffman/mixedmodellint/internal/gopackages"
	"github.com/StevenACoffman/mixedmodellint/internal/jsonschema"
	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
	"github.com/StevenACoffman/mixedmodellint/internal/postgres"
	"github.com/StevenACoffman/mixedmodellint/internal/sidecar"
)

// SidecarName is the sidecar catalog's file name, read from the root of the
// directory being linted.
const SidecarName = "mml.yaml"

// LockName is the schema lock's file name (rule_plan.md §6, M3), read from
// and written to the root of the directory being linted.
const LockName = "mml.lock.json"

// Options configures a run. The zero value lints with no baseline.
type Options struct {
	// Baseline, when set, is read as a baseline file (see mml.WriteBaseline).
	Baseline io.Reader
	// Base, when set, is a git ref (see git.CheckRef) naming the change's
	// base revision; the directory's tree at that ref is read as the
	// previous model.
	Base string
}

// inputs is every file the walk collected, by adapter, and the Go modules
// it found.
type inputs struct {
	sql     []postgres.File
	schemas []jsonschema.File
	sidecar []byte
	lock    []byte
	// artifacts are the decision records and logical-model records read so
	// far, and problems what went wrong reading the latter.
	artifacts mml.Artifacts
	problems  []mml.Problem
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
	input, err := load(ctx, dir)
	if err != nil {
		return mml.Report{}, &mml.Error{Op: op, Err: err}
	}
	if opts.Baseline != nil {
		input.Baseline, err = mml.ReadBaseline(opts.Baseline)
		if err != nil {
			return mml.Report{}, &mml.Error{Op: op, Err: err}
		}
	}
	if opts.Base != "" {
		base, err := loadRef(ctx, dir, opts.Base)
		if err != nil {
			return mml.Report{}, &mml.Error{Op: op, Err: fmt.Errorf("base: %w", err)}
		}
		input.Base = &base
	}
	return mml.Analyze(&input), nil
}

// Lock loads every source under dir and returns the schema lock that
// records it, for the lock command to write as LockName.
func Lock(ctx context.Context, dir string) (mml.Lock, error) {
	input, err := load(ctx, dir)
	if err != nil {
		return mml.Lock{}, &mml.Error{Op: "lint.Lock", Err: err}
	}
	return mml.NewLock(&input), nil
}

// Annotate loads every source under dir and returns the annotations the
// declaration designs ask for that the model lacks, as an mml.yaml fragment
// (see mml.Stubs), for the annotate command to print.
func Annotate(ctx context.Context, dir string) ([]byte, error) {
	input, err := load(ctx, dir)
	if err != nil {
		return nil, &mml.Error{Op: "lint.Annotate", Err: err}
	}
	out, err := sidecar.RenderStubs(mml.Stubs(&input))
	if err != nil {
		return nil, &mml.Error{Op: "lint.Annotate", Err: err}
	}
	return out, nil
}

// loadRef exports dir's tree at ref to a temporary directory and loads it.
func loadRef(ctx context.Context, dir, ref string) (_ mml.Input, err error) {
	tmp, err := os.MkdirTemp("", "mml-base-")
	if err != nil {
		return mml.Input{}, fmt.Errorf("export %q: %w", ref, err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(tmp)) }()
	if err := git.Export(ctx, dir, ref, tmp); err != nil {
		return mml.Input{}, fmt.Errorf("export %q: %w", ref, err)
	}
	return load(ctx, tmp)
}

// load reads every source and artifact under dir into an Input.
func load(ctx context.Context, dir string) (mml.Input, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return mml.Input{}, fmt.Errorf("resolve %s: %w", dir, err)
	}
	in, err := collect(os.DirFS(root))
	if err != nil {
		return mml.Input{}, err
	}
	goSrc, snippets, err := loadGo(ctx, root, in.goModules())
	if err != nil {
		return mml.Input{}, err
	}
	input := mml.Input{
		Sources: []mml.Source{
			postgres.Load(in.sql, snippets),
			goSrc,
			jsonschema.Load(in.schemas),
			{Problems: in.problems},
		},
		Artifacts: in.artifacts,
	}
	if in.sidecar != nil {
		sc, problems := sidecar.Parse(SidecarName, in.sidecar)
		owners, ownerProblems := sidecar.ParseOwnership(SidecarName, in.sidecar, "")
		input.Sidecar = sc
		input.Artifacts.Ownership = append(input.Artifacts.Ownership, owners...)
		input.Sources = append(input.Sources, mml.Source{
			Problems: slices.Concat(problems, ownerProblems),
		})
	}
	if in.lock != nil {
		l, err := mml.ReadLock(bytes.NewReader(in.lock))
		if err != nil {
			return mml.Input{}, fmt.Errorf("read %s: %w", LockName, err)
		}
		input.Lock = &l
	}
	return input, nil
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
		src.Queries = append(src.Queries, res.Source.Queries...)
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
	return in.store(kind, p, src)
}

// store keeps a file's contents for the adapter or artifact list of its kind.
func (in *inputs) store(kind, p string, src []byte) error {
	switch kind {
	case "sql":
		in.sql = append(in.sql, postgres.File{Path: p, Src: src})
	case "json":
		if jsonschema.IsSchema(p, src) {
			in.schemas = append(in.schemas, jsonschema.File{Path: p, Src: src})
		}
	case "sidecar":
		in.sidecar = src
	case "lock":
		in.lock = src
	default:
		return in.storeArtifact(kind, p, src)
	}
	return nil
}

// storeArtifact keeps a file that is not a model source: CODEOWNERS, an ADR,
// a logical-model record or an ownership record.
func (in *inputs) storeArtifact(kind, p string, src []byte) error {
	switch kind {
	case "codeowners":
		in.artifacts.Codeowners = append(
			in.artifacts.Codeowners,
			mml.ParseCodeowners(string(src))...)
	case "adr":
		in.artifacts.ADRs = append(in.artifacts.ADRs, mml.Document{Path: p, Text: string(src)})
	case "logical":
		recs, problems := sidecar.ParseLogical(p, src)
		in.artifacts.Logical = append(in.artifacts.Logical, recs...)
		in.problems = append(in.problems, problems...)
	case "ownership":
		owner := strings.TrimSuffix(path.Base(p), path.Ext(p))
		recs, problems := sidecar.ParseOwnership(p, src, owner)
		in.artifacts.Ownership = append(in.artifacts.Ownership, recs...)
		in.problems = append(in.problems, problems...)
	default:
		return errors.New("unknown file kind " + kind)
	}
	return nil
}

// fileKind names the adapter or artifact list a path belongs to, or "" for
// a file the linter does not read. The first matching rule wins.
func fileKind(p string) string {
	ext := path.Ext(p)
	rules := []struct {
		kind  string
		match bool
	}{
		{"sidecar", p == SidecarName},
		{"lock", p == LockName},
		{
			"codeowners",
			slices.Contains([]string{"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"}, p),
		},
		{"adr", strings.HasPrefix(p, "docs/adr/") && ext == ".md"},
		{"logical", yamlIn(p, "model/logical/")},
		{"ownership", yamlIn(p, "model/ownership/")},
		{"gomod", path.Base(p) == "go.mod"},
		{"sql", ext == ".sql"},
		{"go", ext == ".go" && !strings.HasSuffix(p, "_test.go")},
		{"json", ext == ".json"},
	}
	for _, r := range rules {
		if r.match {
			return r.kind
		}
	}
	return ""
}

// yamlIn reports whether p is a YAML file under dir.
func yamlIn(p, dir string) bool {
	ext := path.Ext(p)
	return strings.HasPrefix(p, dir) && (ext == ".yaml" || ext == ".yml")
}

// skipDir reports directories that hold no model sources of the project
// itself: VCS metadata, dependencies, build output, and test fixtures.
func skipDir(name string) bool {
	switch name {
	case "node_modules", "vendor", "testdata", "bin", "dist", "target":
		return true
	case ".github":
		return false // where CODEOWNERS usually lives
	default:
		return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
	}
}

// Package git is the adapter for the git command: it validates a ref named
// on the command line and exports a directory's tree at that ref, so a
// change can be compared with its base revision. It is the only package that
// runs git.
package git

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"unicode/utf8"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// CheckRef returns an EINVALID error unless ref is a ref name that
// git check-ref-format --allow-onelevel accepts and that cannot be read as
// an option: a branch, tag or remote-tracking name (main, origin/main,
// v1.2), HEAD, or a commit ID. Revision expressions such as HEAD~1 are
// rejected with the rest of ~ ^ : ? * [ \, space and control characters, so
// a ref that passes is a single argument with no newline in it.
func CheckRef(ref string) error {
	if problem := refProblem(ref); problem != "" {
		return &mml.Error{
			Code:    mml.EINVALID,
			Message: fmt.Sprintf("%q is not a git ref name: %s", ref, problem),
		}
	}
	return nil
}

// Export writes the tree of dir at ref into dst, an empty directory: dir's
// own subtree of its repository, as it was in that commit, without
// .git, ignored files or uncommitted changes.
//
// Requires: dir is inside a git work tree; git is on PATH.
// Ensures:  an invalid ref is EINVALID, and so is a ref git cannot resolve
// to a commit holding dir; a symlink or submodule in the tree is skipped.
func Export(ctx context.Context, dir, ref, dst string) error {
	const op = "git.Export"
	if err := CheckRef(ref); err != nil {
		return &mml.Error{Op: op, Err: err}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Run in dir, git archive writes only dir's subtree, with paths relative
	// to it. The ref passed CheckRef, which is git check-ref-format's grammar
	// with no leading dash, and follows --end-of-options, so it is one
	// revision argument and never an option.
	cmd := exec.CommandContext( //nolint:gosec // G204: ref passed CheckRef (see above)
		ctx, "git", "archive", "--format=tar", "--end-of-options", ref)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return &mml.Error{Op: op, Err: fmt.Errorf("git archive: %w", err)}
	}
	if err := cmd.Start(); err != nil {
		return &mml.Error{Op: op, Err: fmt.Errorf("git archive: %w", err)}
	}
	if err := extract(out, dst); err != nil {
		cancel()
		_ = cmd.Wait() // the extract error is the one to report
		return &mml.Error{Op: op, Err: err}
	}
	if err := cmd.Wait(); err != nil {
		if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
			return &mml.Error{Op: op, Err: &mml.Error{
				Code: mml.EINVALID,
				Message: fmt.Sprintf("git archive %s: %s", ref,
					strings.TrimSpace(stderr.String())),
			}}
		}
		return &mml.Error{Op: op, Err: fmt.Errorf("git archive: %w", err)}
	}
	return nil
}

// refProblem says why ref breaks git check-ref-format's rules, or our extra
// one (no leading dash), or returns "" when it breaks none.
func refProblem(ref string) string {
	switch {
	case ref == "":
		return "it is empty"
	case ref == "@":
		return `it is "@"`
	case !utf8.ValidString(ref):
		return "it is not UTF-8"
	case strings.HasPrefix(ref, "-"):
		return "it begins with a dash"
	case strings.HasPrefix(ref, "/"), strings.HasSuffix(ref, "/"):
		return "it begins or ends with a slash"
	case strings.HasSuffix(ref, "."):
		return "it ends with a dot"
	}
	return contentProblem(ref)
}

// contentProblem says which sequence, character or slash-separated
// component of ref git check-ref-format forbids, or returns "".
func contentProblem(ref string) string {
	for _, seq := range []string{"//", "..", "@{"} {
		if strings.Contains(ref, seq) {
			return fmt.Sprintf("it contains %q", seq)
		}
	}
	for _, r := range ref {
		if r < ' ' || r == '\x7f' || strings.ContainsRune(` ~^:?*[\`, r) {
			return fmt.Sprintf("it contains %q", r)
		}
	}
	for c := range strings.SplitSeq(ref, "/") {
		if strings.HasPrefix(c, ".") || strings.HasSuffix(c, ".lock") {
			return fmt.Sprintf("its component %q begins with a dot or ends with .lock", c)
		}
	}
	return ""
}

// extract writes the directories and regular files of the tar stream r
// under dst, through an os.Root so no entry can land outside it.
func extract(r io.Reader, dst string) (err error) {
	root, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("open %s: %w", dst, err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(h.Name, 0o750); err != nil {
				return fmt.Errorf("extract: %w", err)
			}
		case tar.TypeReg:
			if err := writeFile(root, h.Name, tr); err != nil {
				return err
			}
		}
	}
}

// writeFile copies src to name under root, making its directory first.
func writeFile(root *os.Root, name string, src io.Reader) (err error) {
	if err := root.MkdirAll(path.Dir(name), 0o750); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	f, err := root.Create(name)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err := io.Copy(f, src); err != nil {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	return nil
}

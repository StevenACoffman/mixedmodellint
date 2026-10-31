package lint

import (
	"fmt"
	"io"
	"strings"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
	"github.com/StevenACoffman/mixedmodellint/internal/sarif"
)

// Output formats.
const (
	FormatText Format = iota + 1
	FormatJSON
	FormatSARIF
)

// Format is an output format for Render.
type Format int

// Tool identifies the program in SARIF output.
type Tool = sarif.Tool

// ParseFormat returns the Format named by s: "text", "json" or "sarif".
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "sarif":
		return FormatSARIF, nil
	default:
		return 0, &mml.Error{
			Code:    mml.EINVALID,
			Message: fmt.Sprintf("unknown format %q (want text, json or sarif)", s),
		}
	}
}

// Render writes r to w in format f. Text and JSON come from the core; SARIF
// comes from the internal/sarif adapter. tool names the program in SARIF output
// and is ignored by the other formats.
func Render(w io.Writer, r mml.Report, f Format, tool Tool) error {
	const op = "lint.Render"
	var err error
	switch f {
	case FormatText:
		err = mml.WriteText(w, r)
	case FormatJSON:
		err = mml.WriteJSON(w, r)
	case FormatSARIF:
		err = sarif.Write(w, r, tool)
	default:
		return &mml.Error{Op: op, Err: &mml.Error{Code: mml.EINVALID, Message: "unknown format"}}
	}
	if err != nil {
		return &mml.Error{Op: op, Err: err}
	}
	return nil
}

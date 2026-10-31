// Package sarif writes a lint report as SARIF 2.1.0, the format GitHub code
// scanning reads. It wraps github.com/owenrumney/go-sarif/v3, so the document
// structure follows that library's model of the specification rather than a
// hand-written copy of it.
package sarif

import (
	"fmt"
	"io"

	"github.com/owenrumney/go-sarif/v3/pkg/report/v210/sarif"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// fingerprintKey names the partial fingerprint that carries a finding's
// baseline fingerprint, so code scanning tracks a finding across commits the
// same way the baseline does.
const fingerprintKey = "mixedmodellint/v1"

// Tool identifies the program that produced the report.
type Tool struct {
	Name    string
	Version string
	URI     string
}

// Write writes r to w as a SARIF 2.1.0 log with one rule per design that
// produced a finding, in order of first appearance.
func Write(w io.Writer, r mml.Report, tool Tool) error {
	run := sarif.NewRunWithInformationURI(tool.Name, tool.URI)
	if tool.Version != "" {
		run.Tool.Driver.WithVersion(tool.Version)
	}
	if tool.URI == "" {
		// An empty informationUri is not a valid URI, so leave it out.
		run.Tool.Driver.InformationURI = nil
	}
	for i := range r.Findings {
		addResult(run, &r.Findings[i])
	}
	report := sarif.NewReport()
	report.AddRun(run)
	if err := report.PrettyWrite(w); err != nil {
		return fmt.Errorf("sarif.Write: %w", err)
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("sarif.Write: %w", err)
	}
	return nil
}

func addResult(run *sarif.Run, f *mml.Finding) {
	level := f.Severity.String()
	run.AddRule(f.Design).
		WithName(f.Title).
		WithShortDescription(sarif.NewMultiformatMessageString().WithText(ruleLabel(f) + " " + f.Title)).
		WithDefaultConfiguration(sarif.NewReportingConfiguration().WithLevel(level))
	result := run.CreateResultForRule(f.Design).
		WithLevel(level).
		WithMessage(sarif.NewTextMessage(f.Measured + ". " + f.Suggestion)).
		WithPartialFingerprints(map[string]string{fingerprintKey: f.Fingerprint()})
	if loc := location(f.Pos); loc != nil {
		result.AddLocation(loc)
	}
}

// ruleLabel is how a rule is cited in its description: "§7.1:" for a data
// model rule, "mixedmodellint:" for the linter's own findings.
func ruleLabel(f *mml.Finding) string {
	if f.Rule == "mml" {
		return "mixedmodellint:"
	}
	return "§" + f.Rule + ":"
}

// location returns a finding's SARIF location: nil without a file, a
// file-level location without a line, and a region otherwise.
func location(p mml.Position) *sarif.Location {
	if p.File == "" {
		return nil
	}
	phys := sarif.NewPhysicalLocation().
		WithArtifactLocation(sarif.NewSimpleArtifactLocation(p.File))
	if p.Line > 0 {
		region := sarif.NewRegion().WithStartLine(p.Line)
		if p.Col > 0 {
			region.WithStartColumn(p.Col)
		}
		phys.WithRegion(region)
	}
	return sarif.NewLocationWithPhysicalLocation(phys)
}

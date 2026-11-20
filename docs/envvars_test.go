package docs

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var envPattern = regexp.MustCompile(`IBM_AGENTID_[A-Z0-9_]+`)

// codeVars returns the IBM_AGENTID_* names that appear in non-test Go files.
func codeVars(t *testing.T) []string {
	t.Helper()

	var found []string
	err := filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		found = append(found, envPattern.FindAllString(string(src), -1)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(found)
	return slices.Compact(found)
}

// docVars returns the variables listed in the page, split into implemented
// ones and those under the "Planned" heading.
func docVars(t *testing.T) (implemented, planned []string) {
	t.Helper()

	data, err := os.ReadFile("environment-variables.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	head, tail, hasPlanned := strings.Cut(text, "\n## Planned")
	if !hasPlanned {
		t.Fatal(`environment-variables.md must have a "## Planned" section`)
	}

	// Only count names written as code (`NAME`) in table rows, so prose and
	// links that mention a variable do not count as documenting it.
	rowVar := regexp.MustCompile("(?m)^\\|[^\\n]*`(IBM_AGENTID_[A-Z0-9_]+)`")
	collect := func(s string) []string {
		var out []string
		for _, m := range rowVar.FindAllStringSubmatch(s, -1) {
			out = append(out, m[1])
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	return collect(head), collect(tail)
}

func TestEveryVariableReadByCodeIsDocumented(t *testing.T) {
	implemented, planned := docVars(t)
	documented := append(slices.Clone(implemented), planned...)

	for _, v := range codeVars(t) {
		if !slices.Contains(documented, v) {
			t.Errorf("%s is used in code but missing from docs/environment-variables.md", v)
		}
	}
}

func TestImplementedVariablesAreUsedByCode(t *testing.T) {
	implemented, _ := docVars(t)
	code := codeVars(t)

	for _, v := range implemented {
		if !slices.Contains(code, v) {
			t.Errorf("%s is documented as implemented but no code uses it; remove it or list it under Planned", v)
		}
	}
}

func TestPlannedVariablesAreNotYetUsedByCode(t *testing.T) {
	_, planned := docVars(t)
	code := codeVars(t)

	for _, v := range planned {
		if slices.Contains(code, v) {
			t.Errorf("%s is now used in code; move it out of the Planned section", v)
		}
	}
}

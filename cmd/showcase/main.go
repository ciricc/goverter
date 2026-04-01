// showcase generates an HTML page showing all goverter test scenarios
// side-by-side: original comment-based input, migrated DSL, and generated converter output.
//
// Usage:
//
//	go run ./cmd/showcase > showcase.html
package main

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/jmattheis/goverter/comments"
	"github.com/jmattheis/goverter/config"
	"github.com/jmattheis/goverter/dslmigrate"
	"gopkg.in/yaml.v3"
)

func main() {
	root := repoRoot()
	scenarioDir := filepath.Join(root, "scenario")
	workDir := filepath.Join(root, "execution_showcase")

	entries, err := os.ReadDir(scenarioDir)
	must(err)
	must(os.MkdirAll(workDir, 0o755))

	var scenarios []ScenarioEntry
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		entry := buildEntry(name, filepath.Join(scenarioDir, e.Name()), workDir, root)
		if entry != nil {
			scenarios = append(scenarios, *entry)
		}
	}

	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Name < scenarios[j].Name
	})

	html, err := renderHTML(scenarios)
	must(err)
	fmt.Print(html)
}

type ScenarioEntry struct {
	Name        string
	InputFiles  []InputFile // original comment-based files
	DSLFiles    []InputFile // after migration: stripped input files + dsl_gen.go
	Generated   string
	SkipReason  string // non-empty if DSL migration was skipped
}

type InputFile struct {
	Name    string
	Content string
}

type scenarioYAML struct {
	VersionDependent bool              `yaml:"version_dependent,omitempty"`
	Input            map[string]string `yaml:"input"`
	Global           []string          `yaml:"global,omitempty"`
	Patterns         []string          `yaml:"patterns,omitempty"`
	Success          []map[string]string `yaml:"success,omitempty"`
	Error            string            `yaml:"error,omitempty"`
}

func buildEntry(name, scenarioFile, workDir, root string) *ScenarioEntry {
	data, err := os.ReadFile(scenarioFile)
	if err != nil {
		return nil
	}
	var sc scenarioYAML
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return nil
	}

	entry := &ScenarioEntry{Name: name}

	// Collect input files sorted by name
	var inputNames []string
	for n := range sc.Input {
		if n == "go.mod" {
			continue
		}
		inputNames = append(inputNames, n)
	}
	sort.Strings(inputNames)
	for _, n := range inputNames {
		entry.InputFiles = append(entry.InputFiles, InputFile{Name: n, Content: sc.Input[n]})
	}

	if sc.Error != "" {
		return nil // skip error scenarios — not relevant for DSL showcase
	}

	// Collect expected generated output (first success file)
	for _, m := range sc.Success {
		for _, content := range m {
			entry.Generated = content
			break
		}
		break
	}

	// Skip cases we can't migrate
	if needsDSLDep(sc.Input) || len(sc.Success) == 0 {
		entry.SkipReason = skipReason(sc)
		return entry
	}

	// Set up temp workspace
	testWorkDir := filepath.Join(workDir, name)
	must(os.MkdirAll(testWorkDir, 0o755))
	must(clearDir(testWorkDir))

	goMod := "module github.com/jmattheis/goverter/execution\ngo 1.23\nrequire github.com/jmattheis/goverter v0.0.0\nreplace github.com/jmattheis/goverter => " + root
	must(os.WriteFile(filepath.Join(testWorkDir, "go.mod"), []byte(goMod), 0o644))

	for n, content := range sc.Input {
		if strings.HasPrefix(n, "generated/") && strings.Contains(content, "ConverterImpl") {
			continue
		}
		p := filepath.Join(testWorkDir, n)
		must(os.MkdirAll(filepath.Dir(p), 0o755))
		must(os.WriteFile(p, []byte(content), 0o644))
	}

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = testWorkDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		entry.SkipReason = "go mod tidy failed: " + string(out)
		return entry
	}

	patterns := sc.Patterns
	if len(patterns) == 0 {
		patterns = []string{"github.com/jmattheis/goverter/execution"}
	}
	convs, err := comments.ParseDocs(comments.ParseDocsConfig{
		PackagePattern: patterns,
		WorkingDir:     testWorkDir,
		BuildTags:      "goverter",
	})
	if err != nil || len(convs) == 0 {
		entry.SkipReason = "no converters found"
		return entry
	}

	for _, cc := range convs {
		if cc.InterfaceName == "" {
			entry.SkipReason = "variable-based converter"
			return entry
		}
	}

	var jenConvs []jen.Code
	for _, cc := range convs {
		info := dslmigrate.ExtractMethodInfo(cc, testWorkDir, "goverter")
		jenConvs = append(jenConvs, dslmigrate.ConvToJen(cc, info, testWorkDir))
	}
	dslCode := dslmigrate.RenderDSLFile(convs[0].PackageName, jenConvs)

	// Build DSLFiles: stripped input files + dsl_gen.go
	var dslFiles []InputFile
	for _, f := range entry.InputFiles {
		dslFiles = append(dslFiles, InputFile{
			Name:    f.Name,
			Content: stripGoverterComments(f.Content),
		})
	}
	dslFiles = append(dslFiles, InputFile{Name: "dsl_gen.go", Content: dslCode})
	entry.DSLFiles = dslFiles

	return entry
}

func skipReason(sc scenarioYAML) string {
	switch {
	case needsDSLDep(sc.Input):
		return "already DSL-based"
	case len(sc.Success) == 0:
		return "no success output"
	default:
		return "skipped"
	}
}

func stripGoverterComments(src string) string {
	var result []string
	for _, line := range strings.Split(src, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "// goverter:") {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

func needsDSLDep(input map[string]string) bool {
	for _, content := range input {
		if strings.Contains(content, "goverter/dsl") {
			return true
		}
	}
	return false
}

func clearDir(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := os.RemoveAll(f); err != nil {
			return err
		}
	}
	return nil
}

func repoRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "../..")
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// --- HTML rendering ---

func renderHTML(scenarios []ScenarioEntry) (string, error) {
	var successCount, skipCount int
	for _, s := range scenarios {
		if s.SkipReason != "" {
			skipCount++
		} else {
			successCount++
		}
	}

	type tmplData struct {
		Scenarios    []ScenarioEntry
		SuccessCount int
		SkipCount    int
		Total        int
	}

	t := template.Must(template.New("showcase").Funcs(template.FuncMap{
		"join": func(files []InputFile) string {
			var b strings.Builder
			for _, f := range files {
				if len(files) > 1 {
					b.WriteString("// === " + f.Name + " ===\n")
				}
				b.WriteString(f.Content)
				b.WriteString("\n")
			}
			return b.String()
		},
		"hasContent": func(s string) bool { return strings.TrimSpace(s) != "" },
		"slug": func(s string) string {
			return strings.ReplaceAll(s, "_", "-")
		},
	}).Parse(htmlTemplate))

	var buf bytes.Buffer
	err := t.Execute(&buf, tmplData{
		Scenarios:    scenarios,
		SuccessCount: successCount,
		SkipCount:    skipCount,
		Total:        len(scenarios),
	})
	return buf.String(), err
}

// Separate var so the backtick template doesn't conflict with Go string quoting.
var _ = config.RawLines{} // keep config import used

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>goverter DSL Showcase</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #0f1117; color: #e2e8f0; }

  header { padding: 24px 32px; border-bottom: 1px solid #1e2535; display: flex; align-items: center; gap: 24px; }
  header h1 { font-size: 1.4rem; font-weight: 600; color: #fff; }
  .stats { display: flex; gap: 12px; margin-left: auto; }
  .badge { padding: 4px 10px; border-radius: 20px; font-size: 0.75rem; font-weight: 600; }
  .badge-success { background: #14532d; color: #86efac; }
  .badge-error   { background: #450a0a; color: #fca5a5; }
  .badge-skip    { background: #1c1f2e; color: #94a3b8; }

  .search-bar { padding: 16px 32px; border-bottom: 1px solid #1e2535; }
  .search-bar input { width: 100%; max-width: 480px; padding: 8px 14px; border-radius: 8px;
    background: #1e2535; border: 1px solid #2d3748; color: #e2e8f0; font-size: 0.9rem; outline: none; }
  .search-bar input:focus { border-color: #4f6ef7; }

  .scenario-list { padding: 24px 32px; display: flex; flex-direction: column; gap: 16px; }

  .scenario { border: 1px solid #1e2535; border-radius: 12px; overflow: hidden; }
  .scenario-header { padding: 12px 20px; background: #161b27; display: flex; align-items: center;
    gap: 12px; cursor: pointer; user-select: none; }
  .scenario-header:hover { background: #1a2030; }
  .scenario-name { font-weight: 600; font-size: 0.95rem; color: #c9d6f0; flex: 1; }
  .scenario-tag { font-size: 0.72rem; padding: 2px 8px; border-radius: 10px; font-weight: 500; }
  .tag-success { background: #14532d33; color: #86efac; border: 1px solid #14532d; }
  .tag-error   { background: #450a0a33; color: #fca5a5; border: 1px solid #450a0a; }
  .tag-skip    { background: #1c2030; color: #64748b; border: 1px solid #2d3748; }
  .chevron { color: #4a5568; font-size: 0.8rem; transition: transform 0.2s; }
  .scenario.open .chevron { transform: rotate(90deg); }

  .scenario-body { display: none; padding: 0; }
  .scenario.open .scenario-body { display: block; }

  .columns { display: grid; gap: 1px; background: #1e2535; }
  .columns-3 { grid-template-columns: 1fr 1fr 1fr; }
  .columns-2 { grid-template-columns: 1fr 1fr; }
  .column { background: #0f1117; }
  .col-header { padding: 8px 16px; background: #161b27; font-size: 0.72rem; font-weight: 600;
    text-transform: uppercase; letter-spacing: 0.05em; color: #4f6ef7; border-bottom: 1px solid #1e2535; }
  .col-header.dsl  { color: #a78bfa; }
  .col-header.out  { color: #34d399; }
  pre { margin: 0; padding: 16px; font-size: 0.78rem; line-height: 1.6; overflow-x: auto;
    font-family: "JetBrains Mono", "Fira Code", "Cascadia Code", monospace; color: #cbd5e1; }

  .error-box { padding: 16px 20px; background: #1a0a0a; border-top: 1px solid #450a0a; }
  .error-box .label { font-size: 0.72rem; font-weight: 600; text-transform: uppercase;
    letter-spacing: 0.05em; color: #f87171; margin-bottom: 6px; }
  .error-box pre { padding: 0; color: #fca5a5; background: transparent; font-size: 0.8rem; }

  .skip-box { padding: 12px 20px; color: #64748b; font-size: 0.82rem; font-style: italic;
    border-top: 1px solid #1e2535; }
</style>
</head>
<body>

<header>
  <h1>goverter DSL Showcase</h1>
  <div class="stats">
    <span class="badge badge-success">{{.SuccessCount}} migrated</span>
    <span class="badge badge-skip">{{.SkipCount}} skipped</span>
    <span class="badge" style="background:#1c2030;color:#94a3b8">{{.Total}} total</span>
  </div>
</header>

<div class="search-bar">
  <input type="text" id="search" placeholder="Filter scenarios..." oninput="filterScenarios(this.value)">
</div>

<div class="scenario-list" id="list">
{{range .Scenarios}}
<div class="scenario" id="sc-{{slug .Name}}" data-name="{{.Name}}">
  <div class="scenario-header" onclick="toggle(this.parentElement)">
    <span class="chevron">▶</span>
    <span class="scenario-name">{{.Name}}</span>
    {{if .SkipReason}}<span class="scenario-tag tag-skip">{{.SkipReason}}</span>
    {{else}}<span class="scenario-tag tag-success">DSL migrated</span>{{end}}
  </div>
  <div class="scenario-body">
    {{if .SkipReason}}
      <div class="columns columns-1" style="grid-template-columns:1fr">
        <div class="column">
          <div class="col-header">Input</div>
          <pre>{{join .InputFiles}}</pre>
        </div>
      </div>
      <div class="skip-box">DSL migration skipped: {{.SkipReason}}</div>
    {{else}}
      <div class="columns columns-3">
        <div class="column">
          <div class="col-header">Original (comment-based)</div>
          <pre>{{join .InputFiles}}</pre>
        </div>
        <div class="column">
          <div class="col-header dsl">DSL (migrated)</div>
          <pre>{{join .DSLFiles}}</pre>
        </div>
        <div class="column">
          <div class="col-header out">Generated converter</div>
          <pre>{{.Generated}}</pre>
        </div>
      </div>
    {{end}}
  </div>
</div>
{{end}}
</div>

<script>
function toggle(el) {
  el.classList.toggle('open');
}
function filterScenarios(q) {
  q = q.toLowerCase();
  document.querySelectorAll('.scenario').forEach(function(el) {
    el.style.display = el.dataset.name.includes(q) ? '' : 'none';
  });
}
// Open first scenario by default
var first = document.querySelector('.scenario');
if (first) first.classList.add('open');
</script>
</body>
</html>`

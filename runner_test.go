package goverter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/dave/jennifer/jen"
	"github.com/jmattheis/goverter/comments"
	"github.com/jmattheis/goverter/config"
	"github.com/jmattheis/goverter/dslmigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var (
	UpdateScenario       = os.Getenv("UPDATE_SCENARIO") == "true"
	SkipVersionDependent = os.Getenv("SKIP_VERSION_DEPENDENT") == "true"
	NoParallel           = os.Getenv("NO_PARALLEL") == "true"
	TestDSLEquivalence   = os.Getenv("TEST_DSL_EQUIVALENCE") == "true"
)

func TestScenario(t *testing.T) {
	rootDir := getCurrentPath()
	scenarioDir := filepath.Join(rootDir, "scenario")
	workDir := filepath.Join(rootDir, "execution")
	scenarioFiles, err := os.ReadDir(scenarioDir)
	require.NoError(t, err)
	require.NoError(t, clearDir(workDir))

	for _, file := range scenarioFiles {
		require.False(t, file.IsDir(), "should not be a directory")
		file := file

		testName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))

		t.Run(testName, func(t *testing.T) {
			if !NoParallel {
				t.Parallel()
			}
			testWorkDir := filepath.Join(workDir, testName)
			require.NoError(t, os.MkdirAll(testWorkDir, 0o755))
			require.NoError(t, clearDir(testWorkDir))
			scenarioFilePath := filepath.Join(scenarioDir, file.Name())
			scenarioFileBytes, err := os.ReadFile(scenarioFilePath)
			require.NoError(t, err)

			scenario := Scenario{}
			err = yaml.Unmarshal(scenarioFileBytes, &scenario)
			require.NoError(t, err)

			if SkipVersionDependent && scenario.VersionDependent {
				t.SkipNow()
				return
			}

			goMod := "module github.com/jmattheis/goverter/execution\ngo 1.18"
			if needsDSLDep(scenario.Input) {
				goMod = "module github.com/jmattheis/goverter/execution\ngo 1.23\nrequire github.com/jmattheis/goverter v0.0.0\nreplace github.com/jmattheis/goverter => " + rootDir
			}
			err = os.WriteFile(filepath.Join(testWorkDir, "go.mod"), []byte(goMod), 0o644)
			require.NoError(t, err)

			for name, content := range scenario.Input {
				inPath := filepath.Join(testWorkDir, name)
				err = os.MkdirAll(filepath.Dir(inPath), 0o755)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(testWorkDir, name), []byte(content), 0o644)
				require.NoError(t, err)
			}

			if needsDSLDep(scenario.Input) {
				cmd := exec.Command("go", "mod", "tidy")
				cmd.Dir = testWorkDir
				out, tidyErr := cmd.CombinedOutput()
				require.NoError(t, tidyErr, "go mod tidy failed: %s", string(out))
			}

			patterns := scenario.Patterns
			if len(patterns) == 0 {
				patterns = append(patterns, "github.com/jmattheis/goverter/execution")
			}

			files, err := generateConvertersRaw(
				&GenerateConfig{
					WorkingDir:            testWorkDir,
					PackagePatterns:       patterns,
					OutputBuildConstraint: scenario.BuildConstraint,
					BuildTags:             "goverter",
					Global: config.RawLines{
						Lines:    scenario.Global,
						Location: "scenario global",
					},
				})

			actualOutputFiles := toOutputFiles(testWorkDir, files)

			if UpdateScenario {
				if err != nil {
					scenario.Success = []*OutputFile{}
					scenario.Error = replaceAbsolutePath(testWorkDir, fmt.Sprint(err))
				} else {
					scenario.Success = toOutputFiles(testWorkDir, files)
					scenario.Error = ""
				}
				newBytes, err := yaml.Marshal(&scenario)
				if assert.NoError(t, err) {
					os.WriteFile(scenarioFilePath, newBytes, 0o644)
				}
			}

			if scenario.Error != "" {
				require.Error(t, err)
				require.Equal(t, scenario.Error, replaceAbsolutePath(testWorkDir, fmt.Sprint(err)))
				return
			}

			require.NoError(t, err)
			require.NotEmpty(t, scenario.Success, "scenario.Success may not be empty")
			require.Equal(t, scenario.Success, actualOutputFiles)

			err = writeFiles(files)
			require.NoError(t, err)
			require.NoError(t, compile(testWorkDir), "generated converter doesn't build")
		})
	}
}

func replaceAbsolutePath(curPath, body string) string {
	return filepath.ToSlash(strings.ReplaceAll(body, curPath, "@workdir"))
}

func compile(dir string) error {
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	_, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("Process exited with %d:\n%s", exit.ExitCode(), string(exit.Stderr))
		}
	}
	return err
}

func toOutputFiles(execDir string, files map[string][]byte) []*OutputFile {
	output := []*OutputFile{}
	for fileName, content := range files {
		rel, err := filepath.Rel(execDir, fileName)
		if err != nil {
			panic("could not create relpath")
		}
		output = append(output, &OutputFile{Name: filepath.ToSlash(rel), Content: string(content)})
	}
	sort.Slice(output, func(i, j int) bool {
		return output[i].Name < output[j].Name
	})
	return output
}

type Scenario struct {
	VersionDependent bool `yaml:"version_dependent,omitempty"`

	Input  map[string]string `yaml:"input"`
	Global []string          `yaml:"global,omitempty"`

	BuildConstraint string `yaml:"build_constraint,omitempty"`

	Patterns []string      `yaml:"patterns,omitempty"`
	Success  []*OutputFile `yaml:"success,omitempty"`

	Error string `yaml:"error,omitempty"`
}

type OutputFile struct {
	Name    string
	Content string
}

func (f *OutputFile) MarshalYAML() (interface{}, error) {
	return map[string]string{f.Name: f.Content}, nil
}

func (f *OutputFile) UnmarshalYAML(value *yaml.Node) error {
	v := map[string]string{}
	err := value.Decode(&v)

	for name, content := range v {
		f.Name = name
		f.Content = content
	}

	return err
}

func getCurrentPath() string {
	_, filename, _, _ := runtime.Caller(1)

	return filepath.Dir(filename)
}

func needsDSLDep(input map[string]string) bool {
	for _, content := range input {
		if strings.Contains(content, "goverter/dsl") {
			return true
		}
	}
	return false
}

// TestDSLEquiv verifies that for every success scenario, converting
// comment-based converters to DSL and parsing them back produces
// identical RawConverter output. Run with TEST_DSL_EQUIVALENCE=true.
func TestDSLEquiv(t *testing.T) {
	if !TestDSLEquivalence {
		t.Skip("set TEST_DSL_EQUIVALENCE=true to run")
	}

	rootDir := getCurrentPath()
	scenarioDir := filepath.Join(rootDir, "scenario")
	workDir := filepath.Join(rootDir, "execution_dslequiv")
	scenarioFiles, err := os.ReadDir(scenarioDir)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, clearDir(workDir))

	for _, file := range scenarioFiles {
		file := file
		testName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))

		t.Run(testName, func(t *testing.T) {
			if !NoParallel {
				t.Parallel()
			}

			scenarioFileBytes, err := os.ReadFile(filepath.Join(scenarioDir, file.Name()))
			require.NoError(t, err)

			scenario := Scenario{}
			require.NoError(t, yaml.Unmarshal(scenarioFileBytes, &scenario))

			// Skip error scenarios, version-dependent, multi-pattern, and DSL-based
			if scenario.Error != "" || scenario.VersionDependent || len(scenario.Patterns) > 1 || needsDSLDep(scenario.Input) {
				t.Skip("not applicable")
			}
			if len(scenario.Success) == 0 {
				t.Skip("no success output")
			}

			// 1. Set up workspace and parse comments
			testWorkDir := filepath.Join(workDir, testName)
			require.NoError(t, os.MkdirAll(testWorkDir, 0o755))
			require.NoError(t, clearDir(testWorkDir))

			goMod := "module github.com/jmattheis/goverter/execution\ngo 1.23\nrequire github.com/jmattheis/goverter v0.0.0\nreplace github.com/jmattheis/goverter => " + rootDir
			require.NoError(t, os.WriteFile(filepath.Join(testWorkDir, "go.mod"), []byte(goMod), 0o644))

			for name, content := range scenario.Input {
				if strings.HasPrefix(name, "generated/") && strings.Contains(content, "ConverterImpl") {
					continue // skip pre-generated files that reference ConverterImpl — it doesn't exist yet
				}
				inPath := filepath.Join(testWorkDir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(inPath), 0o755))
				require.NoError(t, os.WriteFile(inPath, []byte(content), 0o644))
			}

			// Ensure go.mod has dsl dependency and go 1.23+ for generics
			goModPath := filepath.Join(testWorkDir, "go.mod")
			existingMod, _ := os.ReadFile(goModPath)
			modStr := string(existingMod)
			modStr = strings.Replace(modStr, "go 1.18", "go 1.23", 1)
			if !strings.Contains(modStr, "goverter/dsl") {
				modStr += "\nrequire github.com/jmattheis/goverter v0.0.0\nreplace github.com/jmattheis/goverter => " + rootDir + "\n"
			}
			require.NoError(t, os.WriteFile(goModPath, []byte(modStr), 0o644))

			commentConverters, err := comments.ParseDocs(comments.ParseDocsConfig{
				PackagePattern: []string{"github.com/jmattheis/goverter/execution"},
				WorkingDir:     testWorkDir,
				BuildTags:      "goverter",
			})
			require.NoError(t, err, "comment parser failed")
			if len(commentConverters) == 0 {
				t.Skip("no converters found")
			}

			// 2. Generate DSL file from comment-parsed converters
			var jenConvs []jen.Code
			for _, cc := range commentConverters {
				if cc.InterfaceName == "" {
					t.Skip("variable-based converter, not supported")
				}
				methodInfo := dslmigrate.ExtractMethodInfo(cc, testWorkDir, "goverter")
				jenConvs = append(jenConvs, dslmigrate.ConvToJen(cc, methodInfo, testWorkDir))
			}
			dslCode := dslmigrate.RenderDSLFile(commentConverters[0].PackageName, jenConvs)

			// 3. Write DSL file, remove goverter: comments from originals
			// Skip files in generated/ — they reference ConverterImpl which doesn't exist yet.
			dslFile := filepath.Join(testWorkDir, "dsl_gen.go")
			require.NoError(t, os.WriteFile(dslFile, []byte(dslCode), 0o644))

			for name, content := range scenario.Input {
				if name == "go.mod" || (strings.HasPrefix(name, "generated/") && strings.Contains(content, "ConverterImpl")) {
					continue
				}
				stripped := stripGoverterComments(content)
				require.NoError(t, os.WriteFile(filepath.Join(testWorkDir, name), []byte(stripped), 0o644))
			}

			// go mod tidy for dsl dependency
			cmd := exec.Command("go", "mod", "tidy")
			cmd.Dir = testWorkDir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			out, tidyErr := cmd.CombinedOutput()
			require.NoError(t, tidyErr, "go mod tidy failed: %s", string(out))

			// 4. Run goverter on the DSL version
			dslFiles, genErr := generateConvertersRaw(
				&GenerateConfig{
					WorkingDir:            testWorkDir,
					PackagePatterns:       []string{"github.com/jmattheis/goverter/execution"},
					OutputBuildConstraint: scenario.BuildConstraint,
					BuildTags:             "goverter",
					Global: config.RawLines{
						Lines:    scenario.Global,
						Location: "scenario global",
					},
				})
			require.NoError(t, genErr, "goverter failed on DSL version\ngenerated DSL:\n%s", dslCode)

			// 5. Compare generated output with scenario.Success
			dslOutput := toOutputFiles(testWorkDir, dslFiles)
			require.Equal(t, scenario.Success, dslOutput,
				"DSL-generated output differs from comment-generated output\ngenerated DSL:\n%s", dslCode)
		})
	}
}

func stripGoverterComments(src string) string {
	var result []string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "// goverter:") {
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// normalizeLines normalizes whitespace, sorts extend args, resolves
// relative paths, and unescapes backslashes for stable comparison.
func normalizeLines(lines []string) []string {
	var result []string
	for _, l := range lines {
		// Normalize extend: resolve relative paths, strip prefixes.
		// Drop extend lines entirely — regex expansion makes literal comparison unreliable.
		// The real validation is the generated code (TestScenario covers that).
		if strings.HasPrefix(l, "extend ") {
			continue
		}
		// Drop context lines — MethodPassArgs emits them from signature,
		// while comments use arg:context:regex at converter level.
		if strings.HasPrefix(l, "context ") {
			continue
		}
		// Drop arg:context:regex — DSL uses MethodPassArgs instead,
		// which infers context from the method signature automatically.
		if strings.HasPrefix(l, "arg:context:regex ") {
			continue
		}
		// Normalize "map X X" → "map X" (same-name mapping)
		if strings.HasPrefix(l, "map ") {
			parts := strings.Fields(l)
			if len(parts) == 3 && parts[1] == parts[2] {
				l = "map " + parts[2]
			}
		}
		// Normalize flags: "skipCopySameType yes" → "skipCopySameType"
		// Drop "skipCopySameType no" — means disabled, DSL omits it entirely.
		if strings.HasSuffix(l, " no") {
			continue
		}
		l = strings.TrimSuffix(l, " yes")
		// Unescape backslashes (Jennifer vs raw string difference)
		l = strings.ReplaceAll(l, "\\\\", "\\")
		l = strings.ReplaceAll(l, "\\\"", "\"")
		// Normalize whitespace
		l = strings.Join(strings.Fields(l), " ")
		result = append(result, l)
	}
	sort.Strings(result)
	return result
}

func filterMarker(lines []string) []string {
	var result []string
	for _, l := range lines {
		if l != "converter" && l != "variables" {
			result = append(result, l)
		}
	}
	return result
}

func clearDir(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return err
	}
	for _, file := range files {
		err = os.RemoveAll(file)
		if err != nil {
			return err
		}
	}
	return nil
}

// migrate converts goverter comment-based converter definitions to the type-safe DSL.
//
// For each package containing goverter:converter comments, it:
//  1. Generates a goverter_dsl.go file with dsl.Conv[...] definitions (default)
//     OR inserts the DSL inline into the source file after the converter interface (--inline)
//  2. Removes goverter: comments from the original source files
//
// Usage:
//
//	go run github.com/jmattheis/goverter/cmd/migrate [flags] [packages...]
//
// Examples:
//
//	go run github.com/jmattheis/goverter/cmd/migrate ./...
//	go run github.com/jmattheis/goverter/cmd/migrate ./pkg/converters
//	go run github.com/jmattheis/goverter/cmd/migrate -dry-run ./...
//	go run github.com/jmattheis/goverter/cmd/migrate -inline ./...
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/jmattheis/goverter/comments"
	"github.com/jmattheis/goverter/config"
	"github.com/jmattheis/goverter/dslmigrate"
	"golang.org/x/tools/imports"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "print generated DSL without writing files")
	buildTags := flag.String("tags", "goverter", "build tags to use when loading packages")
	inline := flag.Bool("inline", false, "insert DSL into the source file after the converter interface instead of generating goverter_dsl.go")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: goverter-migrate [flags] [packages...]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Normalize patterns: strip trailing /... for filesystem walking
	var roots []string
	for _, p := range flag.Args() {
		roots = append(roots, strings.TrimSuffix(p, "/..."))
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}

	workDir, err := os.Getwd()
	if err != nil {
		fatalf("could not get working directory: %v", err)
	}

	// Find only directories that contain goverter:converter comments.
	// This avoids loading unrelated packages (e.g. those importing generated code
	// with //go:build !goverter) which fail to compile under -tags goverter.
	patterns, err := findConverterDirs(workDir, roots)
	if err != nil {
		fatalf("failed to scan for converters: %v", err)
	}
	if len(patterns) == 0 {
		fmt.Fprintln(os.Stderr, "no goverter:converter definitions found")
		os.Exit(0)
	}

	convs, err := comments.ParseDocs(comments.ParseDocsConfig{
		PackagePattern: patterns,
		WorkingDir:     workDir,
		BuildTags:      *buildTags,
	})
	if err != nil {
		fatalf("failed to parse converters: %v", err)
	}
	if len(convs) == 0 {
		fmt.Fprintln(os.Stderr, "no goverter:converter definitions found")
		os.Exit(0)
	}

	// Group converters by package path
	type pkgGroup struct {
		pkgName  string
		pkgPath  string
		fileName string // first converter file — DSL goes next to it
		convs    []config.RawConverter
	}
	groups := map[string]*pkgGroup{}
	var order []string
	for _, cc := range convs {
		if cc.InterfaceName == "" {
			fmt.Fprintf(os.Stderr, "warning: skipping variable-based converter in %s (not supported by DSL)\n", cc.PackagePath)
			continue
		}
		g, ok := groups[cc.PackagePath]
		if !ok {
			g = &pkgGroup{
				pkgName:  cc.PackageName,
				pkgPath:  cc.PackagePath,
				fileName: cc.FileName,
			}
			groups[cc.PackagePath] = g
			order = append(order, cc.PackagePath)
		}
		g.convs = append(g.convs, cc)
	}

	for _, pkgPath := range order {
		g := groups[pkgPath]

		if *inline {
			migrateInline(g.convs, workDir, *buildTags, *dryRun)
		} else {
			migrateFile(g.pkgName, g.fileName, g.convs, workDir, *buildTags, *dryRun)
		}
	}
}

// migrateFile writes a goverter_dsl.go file and strips comments from source files.
func migrateFile(pkgName, firstFile string, convs []config.RawConverter, workDir, buildTags string, dryRun bool) {
	var jenConvs []jen.Code
	for _, cc := range convs {
		info := dslmigrate.ExtractMethodInfo(cc, workDir, buildTags)
		jenConvs = append(jenConvs, dslmigrate.ConvToJen(cc, info, workDir))
	}
	dslCode := dslmigrate.RenderDSLFile(pkgName, jenConvs)

	dslFile := filepath.Join(filepath.Dir(firstFile), "goverter_dsl.go")

	if dryRun {
		fmt.Printf("=== %s ===\n%s\n", dslFile, dslCode)
		return
	}

	if err := os.WriteFile(dslFile, []byte(dslCode), 0o644); err != nil {
		fatalf("failed to write %s: %v", dslFile, err)
	}
	fmt.Printf("wrote %s\n", dslFile)

	stripped, err := stripPackageComments(filepath.Dir(firstFile))
	if err != nil {
		fatalf("failed to strip comments: %v", err)
	}
	for path, content := range stripped {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			fatalf("failed to write %s: %v", path, err)
		}
		fmt.Printf("stripped %s\n", path)
	}
}

// migrateInline inserts the DSL snippet directly into each source file, after
// the converter interface declaration. Each converter is inserted into its own file.
func migrateInline(convs []config.RawConverter, workDir, buildTags string, dryRun bool) {
	// Group converters by source file
	type fileEntry struct {
		convs []config.RawConverter
	}
	byFile := map[string]*fileEntry{}
	var fileOrder []string
	for _, cc := range convs {
		if _, ok := byFile[cc.FileName]; !ok {
			byFile[cc.FileName] = &fileEntry{}
			fileOrder = append(fileOrder, cc.FileName)
		}
		byFile[cc.FileName].convs = append(byFile[cc.FileName].convs, cc)
	}

	for _, filePath := range fileOrder {
		entry := byFile[filePath]

		src, err := os.ReadFile(filePath)
		if err != nil {
			fatalf("failed to read %s: %v", filePath, err)
		}

		// Build snippet for all converters in this file
		var jenConvs []jen.Code
		for _, cc := range entry.convs {
			info := dslmigrate.ExtractMethodInfo(cc, workDir, buildTags)
			jenConvs = append(jenConvs, dslmigrate.ConvToJen(cc, info, workDir))
		}
		snippet := dslmigrate.RenderDSLSnippet(jenConvs)

		// Find insertion point: end of the last converter interface in this file
		insertOffset, err := findInsertOffset(filePath, src, entry.convs)
		if err != nil {
			fatalf("failed to find insertion point in %s: %v", filePath, err)
		}

		// Strip goverter: comments and insert snippet
		stripped := stripGoverterComments(string(src))
		result := stripped[:insertOffset] + "\n" + snippet + stripped[insertOffset:]

		// Run goimports to fix up imports
		formatted, err := imports.Process(filePath, []byte(result), nil)
		if err != nil {
			fatalf("goimports failed for %s: %v", filePath, err)
		}

		if dryRun {
			fmt.Printf("=== %s ===\n%s\n", filePath, string(formatted))
			continue
		}

		if err := os.WriteFile(filePath, formatted, 0o644); err != nil {
			fatalf("failed to write %s: %v", filePath, err)
		}
		fmt.Printf("inlined %s\n", filePath)
	}
}

// findInsertOffset returns the byte offset in src after the closing brace of
// the last converter interface listed in convs.
func findInsertOffset(filePath string, src []byte, convs []config.RawConverter) (int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, src, 0)
	if err != nil {
		return 0, fmt.Errorf("parse error: %w", err)
	}

	// Build set of interface names to find
	names := map[string]bool{}
	for _, cc := range convs {
		names[cc.InterfaceName] = true
	}

	lastEnd := -1
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := ts.Type.(*ast.InterfaceType); !ok {
				continue
			}
			if names[ts.Name.Name] {
				end := fset.Position(gd.End()).Offset
				if end > lastEnd {
					lastEnd = end
				}
			}
		}
	}

	if lastEnd == -1 {
		// Fallback: append at end of file
		return len(src), nil
	}
	return lastEnd, nil
}

// findConverterDirs walks roots and returns import-path-style patterns for
// directories that contain at least one "goverter:converter" comment.
// This avoids passing unrelated packages to ParseDocs.
func findConverterDirs(workDir string, roots []string) ([]string, error) {
	seen := map[string]bool{}
	var patterns []string

	for _, root := range roots {
		if !filepath.IsAbs(root) {
			root = filepath.Join(workDir, filepath.Clean(strings.TrimPrefix(root, ".")))
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable dirs
			}
			if d.IsDir() {
				if d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if !strings.Contains(string(data), "goverter:converter") {
				return nil
			}
			dir := filepath.Dir(path)
			if seen[dir] {
				return nil
			}
			seen[dir] = true
			rel, err := filepath.Rel(workDir, dir)
			if err != nil {
				return nil
			}
			patterns = append(patterns, "./"+filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return patterns, nil
}

// stripPackageComments removes // goverter: lines from all .go files in dir.
// Returns only files that were actually modified.
func stripPackageComments(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		src := string(data)
		stripped := stripGoverterComments(src)
		if stripped != src {
			result[path] = stripped
		}
	}
	return result, nil
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

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "migrate: "+format+"\n", args...)
	os.Exit(1)
}

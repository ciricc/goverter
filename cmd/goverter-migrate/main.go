// migrate converts goverter comment-based converter definitions to the type-safe DSL.
//
// For each package containing goverter:converter comments, it:
//  1. Generates a goverter_dsl.go file with dsl.Conv[...] definitions
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
package main

import (
	"flag"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/jmattheis/goverter/comments"
	"github.com/jmattheis/goverter/config"
	"github.com/jmattheis/goverter/dslmigrate"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "print generated DSL without writing files")
	buildTags := flag.String("tags", "goverter", "build tags to use when loading packages")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: migrate [flags] [packages...]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	workDir, err := os.Getwd()
	if err != nil {
		fatalf("could not get working directory: %v", err)
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

	fset := token.NewFileSet()
	_ = fset

	for _, pkgPath := range order {
		g := groups[pkgPath]

		// Generate DSL code
		var jenConvs []jen.Code
		for _, cc := range g.convs {
			info := dslmigrate.ExtractMethodInfo(cc, workDir, *buildTags)
			jenConvs = append(jenConvs, dslmigrate.ConvToJen(cc, info, workDir))
		}
		dslCode := dslmigrate.RenderDSLFile(g.pkgName, jenConvs)

		// DSL file goes in the same directory as the first converter file
		dslFile := filepath.Join(filepath.Dir(g.fileName), "goverter_dsl.go")

		if *dryRun {
			fmt.Printf("=== %s ===\n%s\n", dslFile, dslCode)
			continue
		}

		// Write DSL file
		if err := os.WriteFile(dslFile, []byte(dslCode), 0o644); err != nil {
			fatalf("failed to write %s: %v", dslFile, err)
		}
		fmt.Printf("wrote %s\n", dslFile)

		// Strip goverter: comments from original source files in this package
		stripped, err := stripPackageComments(filepath.Dir(g.fileName))
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

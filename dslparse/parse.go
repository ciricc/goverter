// Package dslparse extracts converter definitions from Go DSL calls
// and converts them to config.RawConverter, the same format used by
// the comment-based parser.
package dslparse

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"github.com/jmattheis/goverter/config"
	"github.com/jmattheis/goverter/dsl"
	"golang.org/x/tools/go/packages"
)

var (
	flagsByDSL    = dsl.FlagByDSL()
	convOptsByDSL = dsl.ConverterOptByDSL()
)

const dslPkgPath = "github.com/jmattheis/goverter/dsl"

// Config provides input to ParseDSL.
type Config struct {
	PackagePattern []string
	WorkingDir     string
	BuildTags      string
}

// ParseDSL scans Go packages for dsl.Conv[T](...) calls and returns
// converter definitions in the same format as comments.ParseDocs.
func ParseDSL(c Config) ([]config.RawConverter, error) {
	loadCfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Dir:  c.WorkingDir,
	}
	if c.BuildTags != "" {
		loadCfg.BuildFlags = append(loadCfg.BuildFlags, "-tags", c.BuildTags)
	}
	pkgs, err := packages.Load(loadCfg, c.PackagePattern...)
	if err != nil {
		return nil, err
	}

	var result []config.RawConverter
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("could not load package %s: %s", pkg.PkgPath, pkg.Errors[0])
		}
		converters, err := parsePackage(pkg)
		if err != nil {
			return nil, err
		}
		result = append(result, converters...)
	}
	return result, nil
}

func parsePackage(pkg *packages.Package) ([]config.RawConverter, error) {
	var result []config.RawConverter
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				continue
			}
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok || len(valueSpec.Values) != 1 {
					continue
				}
				conv, ok, err := parseConvCall(pkg, valueSpec.Values[0])
				if err != nil {
					loc := pkg.Fset.Position(valueSpec.Pos())
					return nil, fmt.Errorf("%s: %s", loc, err)
				}
				if ok {
					conv.FileName = pkg.Fset.Position(file.Pos()).Filename
					conv.PackageName = pkg.Types.Name()
					conv.PackagePath = pkg.Types.Path()
					result = append(result, conv)
				}
			}
		}
	}
	return result, nil
}

// parseConvCall checks if expr is a dsl.Conv[T](...) call and extracts config.
func parseConvCall(pkg *packages.Package, expr ast.Expr) (config.RawConverter, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return config.RawConverter{}, false, nil
	}

	// Check if this calls dsl.Conv or Conv (dot-import).
	fnObj := resolveFuncObj(pkg.TypesInfo, call.Fun)
	if fnObj == nil || fnObj.Pkg() == nil || fnObj.Pkg().Path() != dslPkgPath || fnObj.Name() != "Conv" {
		return config.RawConverter{}, false, nil
	}

	// Extract interface name from type argument.
	// The type info stores instantiation data for generic calls.
	ifaceName, err := extractInterfaceName(pkg.TypesInfo, call)
	if err != nil {
		return config.RawConverter{}, false, err
	}

	conv := config.RawConverter{
		InterfaceName: ifaceName,
		Converter: config.RawLines{
			Location: pkg.Fset.Position(call.Pos()).String(),
			Lines:    []string{"converter"},
		},
		Methods: map[string]config.RawLines{},
	}

	// Parse each option argument.
	for _, arg := range call.Args {
		if err := parseOption(pkg, &conv, arg); err != nil {
			return conv, false, err
		}
	}

	return conv, true, nil
}

// extractInterfaceName gets the interface type name from Conv[T]'s type argument.
func extractInterfaceName(info *types.Info, call *ast.CallExpr) (string, error) {
	// info.Instances is map[*ast.Ident]types.Instance.
	// Find the *ast.Ident that represents the Conv function.
	ident := findIdent(call.Fun)
	if ident == nil {
		return "", fmt.Errorf("could not find identifier in Conv call expression")
	}
	inst, found := info.Instances[ident]
	if !found {
		return "", fmt.Errorf("could not resolve type argument for Conv call")
	}
	if inst.TypeArgs == nil || inst.TypeArgs.Len() == 0 {
		return "", fmt.Errorf("Conv call has no type arguments")
	}

	typeArg := inst.TypeArgs.At(0)
	named, ok := typeArg.(*types.Named)
	if !ok {
		return "", fmt.Errorf("Conv type argument must be a named interface, got %s", typeArg)
	}
	return named.Obj().Name(), nil
}

// findIdent extracts the innermost *ast.Ident from an expression.
// Used to look up generic instantiation info from types.Info.Instances.
func findIdent(expr ast.Expr) *ast.Ident {
	switch e := expr.(type) {
	case *ast.Ident:
		return e
	case *ast.IndexExpr:
		return findIdent(e.X)
	case *ast.SelectorExpr:
		return e.Sel
	}
	return nil
}

// resolveFuncObj returns the types.Object for a function call expression.
func resolveFuncObj(info *types.Info, expr ast.Expr) types.Object {
	switch e := expr.(type) {
	case *ast.Ident:
		return info.ObjectOf(e)
	case *ast.SelectorExpr:
		return info.ObjectOf(e.Sel)
	case *ast.IndexExpr:
		// Generic instantiation: Conv[T] — unwrap to get the ident
		return resolveFuncObj(info, e.X)
	}
	return nil
}

// parseOption processes a single option argument inside Conv[T](...).
func parseOption(pkg *packages.Package, conv *config.RawConverter, expr ast.Expr) error {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}

	fnObj := resolveFuncObj(pkg.TypesInfo, call.Fun)
	if fnObj == nil || fnObj.Pkg() == nil || fnObj.Pkg().Path() != dslPkgPath {
		return nil
	}

	name := fnObj.Name()

	// Flag options (converter-level): optional bool arg, default true.
	if comment, ok := flagsByDSL[name]; ok {
		conv.Converter.Lines = append(conv.Converter.Lines, comment+" "+extractBoolAsYesNo(call, 0))
		return nil
	}

	// Converter options with arguments: table-driven.
	if opt, ok := convOptsByDSL[name]; ok {
		switch opt.Arg {
		case dsl.ArgStr:
			conv.Converter.Lines = append(conv.Converter.Lines, opt.Comment+" "+extractStringOrConst(pkg, call, 0))
		case dsl.ArgBool:
			conv.Converter.Lines = append(conv.Converter.Lines, opt.Comment+" "+extractBoolAsYesNo(call, 0))
		case dsl.ArgFunc:
			names, err := extractFuncRefs(pkg, call)
			if err != nil {
				return err
			}
			if len(names) > 0 {
				conv.Converter.Lines = append(conv.Converter.Lines, opt.Comment+" "+strings.Join(names, " "))
			}
		}
		return nil
	}

	// EnumUnknown — typed EnumAction or const ref
	if name == "EnumUnknown" {
		if len(call.Args) > 0 {
			action := extractEnumAction(pkg, call.Args[0])
			conv.Converter.Lines = append(conv.Converter.Lines, "enum:unknown "+action)
		}
		return nil
	}
	// EnumUnknownConst — const ref for default value
	if name == "EnumUnknownConst" {
		if len(call.Args) > 0 {
			val := extractConstName(pkg, call.Args[0])
			conv.Converter.Lines = append(conv.Converter.Lines, "enum:unknown "+val)
		}
		return nil
	}
	// EnumExclude — *regexp.Regexp, extract pattern string
	if name == "EnumExclude" {
		if len(call.Args) > 0 {
			pattern := extractRegexpPattern(call.Args[0])
			conv.Converter.Lines = append(conv.Converter.Lines, "enum:exclude "+pattern)
		}
		return nil
	}

	// Method / MethodPassArgs / MethodAuto / MethodAutoPassArgs
	if name == "Method" || name == "MethodPassArgs" {
		return parseMethodOption(pkg, conv, call, name == "MethodPassArgs")
	}
if name == "ExtendPassArgs" {
		return parseExtendWithContext(pkg, conv, call, false)
	}
	if name == "ExtendAllContext" {
		return parseExtendAllContext(pkg, conv, call)
	}
	if name == "ExtendPkg" {
		return parseExtendPkg(pkg, conv, call)
	}
	if name == "WrapErrorsUsing" {
		return parseWrapErrorsUsing(pkg, conv, call)
	}

	return nil
}

// parseExtendWithContext handles ExtendPassArgs(...) and ExtendAllContext(...).
//
// ExtendPassArgs: first param is source, rest are context.
// ExtendAllContext: all params are potential context (goverter finds source itself).
//
// For each function, goverter:context comments take priority over the default heuristic.
// Generates arg:context:regex from collected context param names + extend.
func parseExtendWithContext(pkg *packages.Package, conv *config.RawConverter, call *ast.CallExpr, allContext bool) error {
	var contextNames []string
	var funcNames []string

	for _, arg := range call.Args {
		name := extractFuncRef(pkg, arg)
		if name == "" {
			continue
		}
		funcNames = append(funcNames, name)

		obj := resolveFuncObj(pkg.TypesInfo, arg)
		if obj == nil {
			continue
		}
		sig, ok := obj.Type().(*types.Signature)
		if !ok {
			continue
		}

		// goverter:context comments take priority.
		explicit := extractFuncContextComments(pkg, arg)
		if len(explicit) > 0 {
			contextNames = append(contextNames, explicit...)
		} else if allContext {
			// ExtendAllContext: all params go into regex, goverter finds source itself.
			for i := 0; i < sig.Params().Len(); i++ {
				if paramName := sig.Params().At(i).Name(); paramName != "" {
					contextNames = append(contextNames, regexp.QuoteMeta(paramName))
				}
			}
		} else {
			// ExtendPassArgs: skip first param (source), rest are context.
			for i := 1; i < sig.Params().Len(); i++ {
				if paramName := sig.Params().At(i).Name(); paramName != "" {
					contextNames = append(contextNames, regexp.QuoteMeta(paramName))
				}
			}
		}
	}

	if len(funcNames) > 0 {
		if len(contextNames) > 0 {
			seen := map[string]bool{}
			var unique []string
			for _, n := range contextNames {
				if !seen[n] {
					seen[n] = true
					unique = append(unique, n)
				}
			}
			pattern := "^(" + strings.Join(unique, "|") + ")$"
			conv.Converter.Lines = append(conv.Converter.Lines, "arg:context:regex "+pattern)
		}
		conv.Converter.Lines = append(conv.Converter.Lines, "extend "+strings.Join(funcNames, " "))
	}
	return nil
}

// parseWrapErrorsUsing handles WrapErrorsUsing(pkg.Wrap) calls.
// Resolves the package path from the argument and emits "wrapErrorsUsing pkg/path".
func parseWrapErrorsUsing(pkg *packages.Package, conv *config.RawConverter, call *ast.CallExpr) error {
	if len(call.Args) == 0 {
		return nil
	}
	obj := resolveFuncObj(pkg.TypesInfo, call.Args[0])
	if obj == nil || obj.Pkg() == nil {
		return fmt.Errorf("WrapErrorsUsing: could not resolve package from argument")
	}
	conv.Converter.Lines = append(conv.Converter.Lines, "wrapErrorsUsing "+obj.Pkg().Path())
	return nil
}

// parseExtendPkg handles ExtendPkg(symbol, pattern?) calls.
// Resolves the package path from the first argument's type info, then emits
// "extend pkg/path:pattern" (or "extend pkg/path:.*" if no pattern given).
func parseExtendPkg(pkg *packages.Package, conv *config.RawConverter, call *ast.CallExpr) error {
	if len(call.Args) == 0 {
		return nil
	}
	obj := resolveFuncObj(pkg.TypesInfo, call.Args[0])
	var pkgPath string
	if obj != nil && obj.Pkg() != nil {
		pkgPath = obj.Pkg().Path()
	} else {
		// Fallback: try to get package path from the type of the expression
		if tv, ok := pkg.TypesInfo.Types[call.Args[0]]; ok && tv.Type != nil {
			t := tv.Type
			// Dereference pointer
			if pt, ok := t.(*types.Pointer); ok {
				t = pt.Elem()
			}
			if named, ok := t.(*types.Named); ok && named.Obj().Pkg() != nil {
				pkgPath = named.Obj().Pkg().Path()
			}
		}
	}
	if pkgPath == "" {
		return fmt.Errorf("ExtendPkg: could not resolve package from first argument")
	}

	pattern := ".*"
	if len(call.Args) > 1 {
		pattern = extractRegexpPattern(call.Args[1])
	}

	conv.Converter.Lines = append(conv.Converter.Lines, "extend "+pkgPath+":"+pattern)
	return nil
}

// parseExtendAllContext handles ExtendAllContext(fn, "ctx", "ctx2", ...) calls.
// The first argument is the function, the remaining string arguments are the
// names of context parameters. Generates arg:context:regex + extend.
func parseExtendAllContext(pkg *packages.Package, conv *config.RawConverter, call *ast.CallExpr) error {
	if len(call.Args) == 0 {
		return nil
	}

	funcName := extractFuncRef(pkg, call.Args[0])
	if funcName == "" {
		return nil
	}

	var contextNames []string
	for _, arg := range call.Args[1:] {
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		s := lit.Value
		if len(s) >= 2 {
			s = s[1 : len(s)-1]
		}
		if s != "" {
			contextNames = append(contextNames, regexp.QuoteMeta(s))
		}
	}

	if len(contextNames) > 0 {
		pattern := "^(" + strings.Join(contextNames, "|") + ")$"
		conv.Converter.Lines = append(conv.Converter.Lines, "arg:context:regex "+pattern)
	}
	conv.Converter.Lines = append(conv.Converter.Lines, "extend "+funcName)
	return nil
}

// extractFuncContextComments finds goverter:context <name> comments on the
// function referenced by expr and returns the quoted param names.
func extractFuncContextComments(pkg *packages.Package, expr ast.Expr) []string {
	obj := resolveFuncObj(pkg.TypesInfo, expr)
	if obj == nil {
		return nil
	}
	// Find the FuncDecl in the syntax tree matching this object.
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil {
				continue
			}
			if pkg.TypesInfo.ObjectOf(fn.Name) != obj {
				continue
			}
			var names []string
			for _, comment := range fn.Doc.List {
				text := strings.TrimPrefix(strings.TrimSpace(comment.Text), "//")
				text = strings.TrimSpace(text)
				if rest, ok := strings.CutPrefix(text, "goverter:context "); ok {
					names = append(names, regexp.QuoteMeta(strings.TrimSpace(rest)))
				}
			}
			return names
		}
	}
	return nil
}

// parseMethodAutoOption handles MethodAuto(...) and MethodAutoPassArgs(...).
func parseMethodAutoOption(pkg *packages.Package, conv *config.RawConverter, call *ast.CallExpr, passArgs bool) error {
	if len(call.Args) < 1 {
		return fmt.Errorf("MethodAuto call requires at least 1 argument")
	}
	methodName, err := extractMethodName(call.Args[0])
	if err != nil {
		return err
	}
	lines := config.RawLines{
		Location: pkg.Fset.Position(call.Pos()).String(),
	}
	if passArgs {
		ctxLines := extractContextParams(pkg, call.Args[0])
		lines.Lines = append(lines.Lines, ctxLines...)
	}
	conv.Methods[methodName] = lines
	return nil
}

// parseMethodOption handles Method(...) and MethodPassArgs(...).
// When passArgs is true, all extra method parameters are emitted as context.
func parseMethodOption(pkg *packages.Package, conv *config.RawConverter, call *ast.CallExpr, passArgs bool) error {
	if len(call.Args) < 2 {
		return fmt.Errorf("Method call requires at least 2 arguments")
	}

	// First arg: method expression like MyConverter.ConvertUser
	methodName, err := extractMethodName(call.Args[0])
	if err != nil {
		return err
	}

	lines := config.RawLines{
		Location: pkg.Fset.Position(call.Pos()).String(),
	}

	// For MethodPassArgs: emit "context <name>" for each extra parameter.
	// Resolve from the interface method signature.
	if passArgs {
		ctxLines := extractContextParams(pkg, call.Args[0])
		lines.Lines = append(lines.Lines, ctxLines...)
	}

	// Second arg: func(m *Mapping[S, T]) { ... }
	funcLit, ok := call.Args[1].(*ast.FuncLit)
	if !ok {
		conv.Methods[methodName] = lines
		return nil
	}

	// Parse the body of the configure function for m.Map(...), m.Ignore(...), etc.
	for _, stmt := range funcLit.Body.List {
		exprStmt, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		innerCall, ok := exprStmt.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		line, err := parseMappingCall(pkg, innerCall)
		if err != nil {
			return err
		}
		if line != "" {
			lines.Lines = append(lines.Lines, line)
		}
	}

	conv.Methods[methodName] = lines
	return nil
}

// parseMappingCall converts m.Map(...), m.Ignore(...), etc. to goverter: lines.
func parseMappingCall(pkg *packages.Package, call *ast.CallExpr) (string, error) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", nil
	}

	name := sel.Sel.Name

	// Typed enum options (method-level)
	if name == "EnumUnknown" && len(call.Args) > 0 {
		action := extractEnumAction(pkg, call.Args[0])
		return "enum:unknown " + action, nil
	}
	if name == "EnumUnknownConst" && len(call.Args) > 0 {
		val := extractConstName(pkg, call.Args[0])
		return "enum:unknown " + val, nil
	}
	if name == "EnumTransform" && len(call.Args) > 0 {
		return "enum:transform " + extractTransformerConfig(pkg, call.Args[0]), nil
	}

	// Flag options (method-level): optional bool arg, default true.
	if comment, ok := flagsByDSL[name]; ok {
		return comment + " " + extractBoolAsYesNo(call, 0), nil
	}

	def, ok := dsl.MethodDefs[name]
	if !ok {
		return "", nil
	}

	// Count how many ADot (literal) args there are — they don't consume call.Args.
	dotCount := 0
	for _, k := range def.Args {
		if k == dsl.ADot {
			dotCount++
		}
	}
	expectedArgs := len(def.Args) - dotCount
	if !def.Variadic && len(call.Args) != expectedArgs {
		return "", fmt.Errorf("%s requires %d arguments, got %d", name, expectedArgs, len(call.Args))
	}

	// Extract each argument, skipping ADot entries (they emit "." without consuming a call arg).
	var parts []string
	callArgIdx := 0
	for defIdx, kind := range def.Args {
		if def.Variadic && defIdx == len(def.Args)-1 {
			// variadic: consume remaining call args with last kind
			for ; callArgIdx < len(call.Args); callArgIdx++ {
				parts = append(parts, extractArg(pkg, call, callArgIdx, kind))
			}
			break
		}
		if kind == dsl.ADot {
			parts = append(parts, ".")
			continue
		}
		parts = append(parts, extractArg(pkg, call, callArgIdx, kind))
		callArgIdx++
	}

	// Build the goverter: line.
	if def.PipeLast && len(parts) >= 2 {
		last := parts[len(parts)-1]
		rest := strings.Join(parts[:len(parts)-1], " ")
		return def.Comment + " " + rest + " | " + last, nil
	}
	return def.Comment + " " + strings.Join(parts, " "), nil
}

func extractArg(pkg *packages.Package, call *ast.CallExpr, i int, kind dsl.MethodArgKind) string {
	switch kind {
	case dsl.AField:
		return extractFieldPath(pkg, call.Args[i])
	case dsl.AFunc:
		return extractFuncRef(pkg, call.Args[i])
	case dsl.AStr:
		return extractString(call, i)
	case dsl.ABool:
		return extractBoolAsYesNo(call, i)
	case dsl.ADot:
		return "."
	}
	return ""
}

// isSourceSentinel checks if expr refers to dsl.Source (via dot-import or qualified).
func isSourceSentinel(pkg *packages.Package, expr ast.Expr) bool {
	var obj types.Object
	switch e := expr.(type) {
	case *ast.Ident:
		obj = pkg.TypesInfo.ObjectOf(e)
	case *ast.SelectorExpr:
		obj = pkg.TypesInfo.ObjectOf(e.Sel)
	default:
		return false
	}
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == dslPkgPath && obj.Name() == "Source"
}

// extractContextParams resolves extra parameters from a method expression
// and returns "context <name>" lines for each one beyond the first (source).
func extractContextParams(pkg *packages.Package, methodExpr ast.Expr) []string {
	sel, ok := methodExpr.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	// Resolve the method's type signature
	obj := pkg.TypesInfo.ObjectOf(sel.Sel)
	if obj == nil {
		return nil
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok {
		return nil
	}
	var lines []string
	// Skip first param (source). Remaining are context.
	for i := 1; i < sig.Params().Len(); i++ {
		name := sig.Params().At(i).Name()
		if name != "" {
			lines = append(lines, "context "+name)
		}
	}
	return lines
}

// extractMethodName gets the method name from a method expression like MyConverter.ConvertUser.
func extractMethodName(expr ast.Expr) (string, error) {
	sel, ok := expr.(*ast.SelectorExpr)
	if ok {
		return sel.Sel.Name, nil
	}
	return "", fmt.Errorf("expected method expression (e.g. MyConverter.Method), got %T", expr)
}

// extractFieldPath resolves m.From.X.Y or m.To.X into a field path string.
// Returns "." for dsl.Source sentinel.
func extractFieldPath(pkg *packages.Package, expr ast.Expr) string {
	// Check for nil
	if ident, ok := expr.(*ast.Ident); ok && ident.Name == "nil" {
		return "."
	}

	// Check for dsl.Source sentinel — could be Ident (dot-import) or SelectorExpr (qualified)
	if isSourceSentinel(pkg, expr) {
		return "."
	}

	// Collect selector chain: m.From.Nested.Field → [m, From, Nested, Field]
	var parts []string
	for {
		sel, ok := expr.(*ast.SelectorExpr)
		if !ok {
			break
		}
		parts = append([]string{sel.Sel.Name}, parts...)
		expr = sel.X
	}

	// Strip "m", "From"/"To" prefix: [From, Nested, Field] → "Nested.Field"
	if len(parts) >= 2 {
		// parts[0] is "From" or "To", the rest is the field path
		return strings.Join(parts[1:], ".")
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "."
}

// extractFuncRef resolves a function reference to "package:FuncName" format.
// Supports Go expressions (strconv.Itoa) and string literals ("generated:intToString").
func extractFuncRef(pkg *packages.Package, expr ast.Expr) string {
	// String literal — pass through as raw ref (for unexported, cycles, regex)
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		s := lit.Value
		if len(s) >= 2 {
			return s[1 : len(s)-1] // strip quotes
		}
		return s
	}
	obj := resolveFuncObj(pkg.TypesInfo, expr)
	if obj == nil {
		return ""
	}
	if obj.Pkg() == nil || obj.Pkg().Path() == pkg.Types.Path() {
		return obj.Name()
	}
	return obj.Pkg().Path() + ":" + obj.Name()
}

// extractFuncRefs resolves all function reference arguments from a call.
// Returns an error if any argument is not a function reference or string literal
// (e.g. a lambda — not supported).
func extractFuncRefs(pkg *packages.Package, call *ast.CallExpr) ([]string, error) {
	var names []string
	for _, arg := range call.Args {
		if _, ok := arg.(*ast.FuncLit); ok {
			return nil, fmt.Errorf("lambda functions are not supported as arguments — use a named function reference instead")
		}
		name := extractFuncRef(pkg, arg)
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// extractEnumAction resolves dsl.EnumPanic/EnumError/EnumIgnore to "@panic"/"@error"/"@ignore".
func extractEnumAction(pkg *packages.Package, expr ast.Expr) string {
	obj := resolveFuncObj(pkg.TypesInfo, expr)
	if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == dslPkgPath {
		switch obj.Name() {
		case "EnumPanic":
			return "@panic"
		case "EnumError":
			return "@error"
		case "EnumIgnore":
			return "@ignore"
		}
	}
	return ""
}

// extractConstName resolves a const reference to its name.
func extractConstName(pkg *packages.Package, expr ast.Expr) string {
	obj := resolveFuncObj(pkg.TypesInfo, expr)
	if obj != nil {
		return obj.Name()
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// extractRegexpPattern extracts the pattern string from regexp.MustCompile("pattern").
func extractRegexpPattern(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return ""
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s := lit.Value
	if len(s) >= 2 && s[0] == '"' {
		return s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '`' {
		return s[1 : len(s)-1]
	}
	return s
}

// extractTransformerConfig extracts "name pattern replacement" from dsl.Regex("pat", "repl").
func extractTransformerConfig(pkg *packages.Package, expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	// Resolve the function name (e.g. dsl.Regex)
	fnObj := resolveFuncObj(pkg.TypesInfo, call.Fun)
	if fnObj == nil {
		return ""
	}
	name := fnObj.Name() // "Regex"
	// Extract string args
	var parts []string
	parts = append(parts, strings.ToLower(name)) // "regex"
	for i := range call.Args {
		parts = append(parts, extractString(call, i))
	}
	return strings.Join(parts, " ")
}

// extractBoolAsYesNo gets a bool literal and converts true→"yes", false→"no".
func extractBoolAsYesNo(call *ast.CallExpr, i int) string {
	if i >= len(call.Args) {
		return "yes"
	}
	ident, ok := call.Args[i].(*ast.Ident)
	if !ok {
		return "yes"
	}
	if ident.Name == "false" {
		return "no"
	}
	return "yes"
}

// extractString gets a string literal from argument at index i.
// extractStringOrConst extracts a string argument that may be either a string
// literal or a typed string constant (e.g. dsl.OutputFormatFunction).
func extractStringOrConst(pkg *packages.Package, call *ast.CallExpr, i int) string {
	if i >= len(call.Args) {
		return ""
	}
	// Try to resolve as a constant value via type info
	if tv, ok := pkg.TypesInfo.Types[call.Args[i]]; ok && tv.Value != nil {
		s := tv.Value.String()
		// constant string values are quoted — strip quotes
		if len(s) >= 2 && s[0] == '"' {
			return s[1 : len(s)-1]
		}
		return s
	}
	return extractString(call, i)
}

func extractString(call *ast.CallExpr, i int) string {
	if i >= len(call.Args) {
		return ""
	}
	lit, ok := call.Args[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	// Remove quotes (double-quoted or backtick)
	s := lit.Value
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

package dslmigrate

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/jmattheis/goverter/config"
	"github.com/jmattheis/goverter/dsl"
	"github.com/jmattheis/goverter/pkgload"
	"golang.org/x/tools/go/packages"
)

const dslPkg = "github.com/jmattheis/goverter/dsl"

// MethodInfo holds source/target type names and needed imports.
type MethodInfo struct {
	Source     string
	Target     string
	SourcePkg  string
	TargetPkg  string
	SourceType jen.Code
	TargetType jen.Code
}

// migrateCtx carries context through the migration call chain.
type migrateCtx struct {
	sourcePkg  string // converter's package path
	workDir    string // workspace directory for package loading
	pkgCache   map[string]*types.Package    // loaded packages cache
	syntaxCache map[string]*packages.Package // loaded packages with syntax
}

func (ctx *migrateCtx) loadPkg(pkgPath string) *types.Package {
	if ctx.pkgCache == nil {
		ctx.pkgCache = map[string]*types.Package{}
	}
	if p, ok := ctx.pkgCache[pkgPath]; ok {
		return p
	}
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes, Dir: ctx.workDir}
	pkgs, err := packages.Load(cfg, pkgPath)
	if err != nil || len(pkgs) == 0 || pkgs[0].Types == nil {
		ctx.pkgCache[pkgPath] = nil
		return nil
	}
	ctx.pkgCache[pkgPath] = pkgs[0].Types
	return pkgs[0].Types
}

func (ctx *migrateCtx) loadPkgWithSyntax(pkgPath string) *packages.Package {
	if ctx.syntaxCache == nil {
		ctx.syntaxCache = map[string]*packages.Package{}
	}
	if p, ok := ctx.syntaxCache[pkgPath]; ok {
		return p
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Dir:  ctx.workDir,
	}
	pkgs, err := packages.Load(cfg, pkgPath)
	if err != nil || len(pkgs) == 0 || pkgs[0].Types == nil {
		ctx.syntaxCache[pkgPath] = nil
		return nil
	}
	ctx.syntaxCache[pkgPath] = pkgs[0]
	return pkgs[0]
}

// extendWithContextToJen generates ExtendPassArgs / ExtendAllContext calls for
// a space-separated list of function refs. Functions where the first param is a
// context param get their own ExtendAllContext(fn, "p1", "p2") call; the rest
// are grouped into a single ExtendPassArgs(f1, f2, ...) call.
func (ctx *migrateCtx) extendWithContextToJen(refs string) []jen.Code {
	var passArgsRefs []jen.Code
	var result []jen.Code

	for _, ref := range strings.Fields(refs) {
		fnExpr := ctx.funcRefsToJen(ref, "", "")
		if len(fnExpr) == 0 {
			continue
		}

		if ctx.funcRefFirstParamIsContext(ref) {
			// Flush accumulated passArgs first.
			if len(passArgsRefs) > 0 {
				result = append(result, jen.Qual(dslPkg, "ExtendPassArgs").Call(passArgsRefs...))
				passArgsRefs = nil
			}
			// ExtendAllContext(fn, "ctx", "ctx2", ...)
			args := []jen.Code{fnExpr[0]}
			for _, name := range ctx.funcRefContextParamNames(ref) {
				args = append(args, jen.Lit(name))
			}
			result = append(result, jen.Qual(dslPkg, "ExtendAllContext").Call(args...))
		} else {
			passArgsRefs = append(passArgsRefs, fnExpr...)
		}
	}

	if len(passArgsRefs) > 0 {
		result = append(result, jen.Qual(dslPkg, "ExtendPassArgs").Call(passArgsRefs...))
	}
	return result
}

// funcRefFirstParamIsContext returns true if the first parameter of the function
// has a goverter:context comment on it.
func (ctx *migrateCtx) funcRefFirstParamIsContext(ref string) bool {
	pkgPath, name, err := pkgload.ParseMethodString(ctx.sourcePkg, ref)
	if err != nil {
		return false
	}
	pkg := ctx.loadPkgWithSyntax(pkgPath)
	if pkg == nil {
		return false
	}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != name || fn.Doc == nil {
				continue
			}
			if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
				return false
			}
			firstParam := ""
			if len(fn.Type.Params.List[0].Names) > 0 {
				firstParam = fn.Type.Params.List[0].Names[0].Name
			}
			for _, comment := range fn.Doc.List {
				text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
				if rest, ok := strings.CutPrefix(text, "goverter:context "); ok {
					if strings.TrimSpace(rest) == firstParam {
						return true
					}
				}
			}
		}
	}
	return false
}

// funcRefContextParamNames returns the names of context parameters from
// goverter:context comments on the function.
func (ctx *migrateCtx) funcRefContextParamNames(ref string) []string {
	pkgPath, name, err := pkgload.ParseMethodString(ctx.sourcePkg, ref)
	if err != nil {
		return nil
	}
	pkg := ctx.loadPkgWithSyntax(pkgPath)
	if pkg == nil {
		return nil
	}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != name || fn.Doc == nil {
				continue
			}
			var names []string
			for _, comment := range fn.Doc.List {
				text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
				if rest, ok := strings.CutPrefix(text, "goverter:context "); ok {
					names = append(names, strings.TrimSpace(rest))
				}
			}
			return names
		}
	}
	return nil
}

// anyExtendFuncHasContext returns true if any function in the space-separated
// refs string has a goverter:context comment.
func (ctx *migrateCtx) anyExtendFuncHasContext(refs string) bool {
	for _, ref := range strings.Fields(refs) {
		pkg, name, err := pkgload.ParseMethodString(ctx.sourcePkg, ref)
		if err != nil {
			continue
		}
		if ctx.funcHasContextComment(pkg, name) {
			return true
		}
	}
	return false
}


// funcHasContextComment returns true if the named function in pkgPath
// has a goverter:context comment on it (same logic as pkgload.localConfig).
func (ctx *migrateCtx) funcHasContextComment(pkgPath, funcName string) bool {
	pkg := ctx.loadPkgWithSyntax(pkgPath)
	if pkg == nil {
		return false
	}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != funcName {
				continue
			}
			if fn.Doc == nil {
				return false
			}
			for _, comment := range fn.Doc.List {
				text := strings.TrimPrefix(comment.Text, "//")
				text = strings.TrimSpace(text)
				if strings.HasPrefix(text, "goverter:context") {
					return true
				}
			}
		}
	}
	return false
}

// lookupFunc finds a function object by package path and name.
func (ctx *migrateCtx) lookupFunc(pkgPath, name string) *types.Func {
	pkg := ctx.loadPkg(pkgPath)
	if pkg == nil {
		return nil
	}
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return nil
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	return fn
}

// ExtractMethodInfo loads the package and extracts source/target type info.
func ExtractMethodInfo(conv config.RawConverter, workDir, buildTags string) map[string]MethodInfo {
	result := map[string]MethodInfo{}
	if conv.InterfaceName == "" {
		return result
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  workDir,
	}
	if buildTags != "" {
		cfg.BuildFlags = append(cfg.BuildFlags, "-tags", buildTags)
	}

	pkgs, err := packages.Load(cfg, conv.PackagePath)
	if err != nil || len(pkgs) == 0 {
		return result
	}
	pkg := pkgs[0]
	if pkg.Types == nil {
		return result
	}

	obj := pkg.Types.Scope().Lookup(conv.InterfaceName)
	if obj == nil {
		return result
	}
	iface, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return result
	}

	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		sig := m.Type().(*types.Signature)

		source, sourcePkg := "any", ""
		target, targetPkg := "any", ""

		if sig.Params().Len() >= 1 {
			source, sourcePkg = qualifiedTypeInfo(sig.Params().At(0).Type(), pkg.Types)
		}

		// Detect update pattern: last param is pointer, no "real" return
		// (void or just error). Target comes from the pointer param.
		isUpdate := sig.Params().Len() >= 2 &&
			isPointerType(sig.Params().At(sig.Params().Len()-1).Type()) &&
			(sig.Results().Len() == 0 || (sig.Results().Len() == 1 && isErrorType(sig.Results().At(0).Type())))

		if isUpdate {
			// Target is the last pointer param, unwrapped
			target, targetPkg = qualifiedTypeInfo(sig.Params().At(sig.Params().Len()-1).Type(), pkg.Types)
		} else if sig.Results().Len() >= 1 && !isErrorType(sig.Results().At(0).Type()) {
			target, targetPkg = qualifiedTypeInfo(sig.Results().At(0).Type(), pkg.Types)
		}

		var sourceTypeJen jen.Code = jen.Id("any")
		if sig.Params().Len() >= 1 {
			sourceTypeJen = typeToJen(sig.Params().At(0).Type(), pkg.Types)
		}
		result[m.Name()] = MethodInfo{
			Source:     source,
			Target:     target,
			SourcePkg:  sourcePkg,
			TargetPkg:  targetPkg,
			SourceType: sourceTypeJen,
			TargetType: targetTypeJen(sig, pkg.Types, isUpdate),
		}
	}
	return result
}

func isPointerType(t types.Type) bool {
	_, ok := t.(*types.Pointer)
	return ok
}

func isErrorType(t types.Type) bool {
	return t.String() == "error"
}

func qualifiedTypeInfo(t types.Type, localPkg *types.Package) (name, pkg string) {
	switch v := t.(type) {
	case *types.Pointer:
		return qualifiedTypeInfo(v.Elem(), localPkg)
	case *types.Slice:
		return qualifiedTypeInfo(v.Elem(), localPkg)
	case *types.Array:
		return qualifiedTypeInfo(v.Elem(), localPkg)
	case *types.Map:
		return qualifiedTypeInfo(v.Elem(), localPkg)
	case *types.Named:
		p := v.Obj().Pkg()
		baseName := v.Obj().Name()
		// Include type args for generic types: X[int] not just X
		if v.TypeArgs() != nil && v.TypeArgs().Len() > 0 {
			var args []string
			for i := 0; i < v.TypeArgs().Len(); i++ {
				argName, _ := qualifiedTypeInfo(v.TypeArgs().At(i), localPkg)
				args = append(args, argName)
			}
			baseName += "[" + strings.Join(args, ", ") + "]"
		}
		if p == nil || p.Path() == localPkg.Path() {
			return baseName, ""
		}
		return baseName, p.Path()
	case *types.Basic:
		return v.Name(), ""
	}
	return "any", ""
}

// typeToJen converts a go/types.Type to a jen.Code expression,
// correctly handling chan, slice, map, pointer, array, and named types.
func typeToJen(t types.Type, localPkg *types.Package) jen.Code {
	switch v := t.(type) {
	case *types.Chan:
		elem := typeToJen(v.Elem(), localPkg)
		switch v.Dir() {
		case types.SendRecv:
			return jen.Chan().Add(elem)
		case types.SendOnly:
			return jen.Chan().Op("<-").Add(elem)
		case types.RecvOnly:
			return jen.Op("<-").Chan().Add(elem)
		}
	case *types.Slice:
		return jen.Index().Add(typeToJen(v.Elem(), localPkg))
	case *types.Array:
		return jen.Index(jen.Lit(int(v.Len()))).Add(typeToJen(v.Elem(), localPkg))
	case *types.Pointer:
		return jen.Op("*").Add(typeToJen(v.Elem(), localPkg))
	case *types.Map:
		return jen.Map(typeToJen(v.Key(), localPkg)).Add(typeToJen(v.Elem(), localPkg))
	case *types.Named:
		// Uninstantiated generic types can't be used as Mapping type args.
		if v.TypeParams() != nil && v.TypeParams().Len() > 0 && (v.TypeArgs() == nil || v.TypeArgs().Len() == 0) {
			return jen.Id("any")
		}
		p := v.Obj().Pkg()
		name := v.Obj().Name()
		var base jen.Code
		if p == nil || p.Path() == localPkg.Path() {
			base = jen.Id(name)
		} else {
			base = jen.Qual(p.Path(), name)
		}
		if v.TypeArgs() != nil && v.TypeArgs().Len() > 0 {
			var args []jen.Code
			for i := 0; i < v.TypeArgs().Len(); i++ {
				args = append(args, typeToJen(v.TypeArgs().At(i), localPkg))
			}
			return base.(*jen.Statement).Types(args...)
		}
		return base
	case *types.Basic:
		return jen.Id(v.Name())
	}
	return jen.Id("any")
}

// targetTypeJen returns the jen.Code for the target type of a method.
func targetTypeJen(sig *types.Signature, localPkg *types.Package, isUpdate bool) jen.Code {
	if isUpdate {
		last := sig.Params().At(sig.Params().Len() - 1).Type()
		if ptr, ok := last.(*types.Pointer); ok {
			return typeToJen(ptr.Elem(), localPkg)
		}
		return typeToJen(last, localPkg)
	}
	if sig.Results().Len() >= 1 && !isErrorType(sig.Results().At(0).Type()) {
		return typeToJen(sig.Results().At(0).Type(), localPkg)
	}
	return jen.Id("any")
}

// ConvToJen returns a jen statement for a single converter definition.
func ConvToJen(conv config.RawConverter, methods map[string]MethodInfo, workDir string) jen.Code {
	ctx := &migrateCtx{sourcePkg: conv.PackagePath, workDir: workDir}

	// Detect converter-level arg:context:regex → all methods use MethodPassArgs
	forcePassArgs := ctx.converterHasContextRegex(conv.Converter.Lines)

	var opts []jen.Code
	for _, line := range conv.Converter.Lines {
		stmts := ctx.converterLineToJenMulti(line, forcePassArgs)
		opts = append(opts, stmts...)
	}
	for name, method := range conv.Methods {
		info := methods[name]
		opts = append(opts, ctx.methodToJen(conv.InterfaceName, name, forcePassArgs, info, method))
	}

	return jen.Var().Id("_").Op("=").Qual(dslPkg, "Conv").Types(jen.Id(conv.InterfaceName)).CustomFunc(jen.Options{
		Open:      "(",
		Close:     ")",
		Separator: ",",
		Multi:     true,
	}, func(g *jen.Group) {
		for _, opt := range opts {
			g.Add(opt)
		}
	})
}

// RenderDSLFile renders multiple converter definitions into a single Go file.
func RenderDSLFile(pkgName string, convs []jen.Code) string {
	f := jen.NewFile(pkgName)
	for i, conv := range convs {
		if i > 0 {
			f.Line()
		}
		f.Add(conv)
	}
	buf := &strings.Builder{}
	if err := f.Render(buf); err != nil {
		return "// render error: " + err.Error()
	}
	return buf.String()
}

func (ctx *migrateCtx) converterLineToJenMulti(line string, passArgs bool) []jen.Code {
	cmd, rest := splitFirst(line)
	if cmd == "extend" && (passArgs || ctx.anyExtendFuncHasContext(rest)) {
		return ctx.extendWithContextToJen(rest)
	}
	stmt := ctx.converterLineToJen(line, passArgs)
	if stmt == nil {
		return nil
	}
	return []jen.Code{stmt}
}

func (ctx *migrateCtx) converterLineToJen(line string, passArgs bool) jen.Code {
	cmd, rest := splitFirst(line)
	if cmd == "converter" || cmd == "variables" || cmd == "arg:context:regex" {
		return nil
	}
	if dslName, ok := dsl.FlagByComment()[cmd]; ok {
		enabled := rest != "no"
		return jen.Qual(dslPkg, dslName).Call(jen.Lit(enabled))
	}
	// Typed enum options at converter level
	if cmd == "enum:unknown" {
		return enumUnknownToJen("", rest)
	}
	if cmd == "enum:exclude" {
		return jen.Qual(dslPkg, "EnumExclude").Call(
			jen.Qual("regexp", "MustCompile").Call(jen.Lit(rest)),
		)
	}

	if opt, ok := dsl.ConverterOptByComment()[cmd]; ok {
		switch opt.Arg {
		case dsl.ArgStr:
			return jen.Qual(dslPkg, opt.DSL).Call(strLit(rest))
		case dsl.ArgBool:
			return jen.Qual(dslPkg, opt.DSL).Call(jen.Lit(rest == "yes" || rest == ""))
		case dsl.ArgFunc:
			var args []jen.Code
			for _, ref := range strings.Fields(rest) {
				args = append(args, ctx.funcRefsToJen(ref, "", "")...)
			}
			return jen.Qual(dslPkg, opt.DSL).Call(args...)
		}
	}
	return jen.Comment("TODO: unsupported converter option: " + line)
}

func (ctx *migrateCtx) converterHasContextRegex(lines []string) bool {
	for _, line := range lines {
		cmd, _ := splitFirst(line)
		if cmd == "arg:context:regex" {
			return true
		}
	}
	return false
}

func (ctx *migrateCtx) methodToJen(ifaceName, methodName string, forcePassArgs bool, info MethodInfo, method config.RawLines) jen.Code {
	srcType := info.SourceType
	tgtType := info.TargetType
	if srcType == nil {
		srcType = typeRefToJen(info.Source, info.SourcePkg)
	}
	if tgtType == nil {
		tgtType = typeRefToJen(info.Target, info.TargetPkg)
	}

	// Detect context lines → use MethodPassArgs, skip context lines
	hasContext := forcePassArgs
	var body []jen.Code
	for _, line := range method.Lines {
		cmd, _ := splitFirst(line)
		if cmd == "context" || cmd == "arg:context:regex" {
			hasContext = true
			continue // MethodPassArgs handles this automatically
		}
		if stmt := ctx.methodLineToJen(line, info); stmt != nil {
			body = append(body, stmt)
		}
	}

	if len(body) == 0 {
		dslFunc := "MethodAuto"
		if hasContext {
			dslFunc = "MethodAutoPassArgs"
		}
		return jen.Qual(dslPkg, dslFunc).Call(jen.Id(ifaceName).Dot(methodName))
	}

	dslFunc := "Method"
	if hasContext {
		dslFunc = "MethodPassArgs"
	}

	callback := jen.Func().Params(
		jen.Id("m").Op("*").Qual(dslPkg, "Mapping").Types(srcType, tgtType),
	).Block(body...)

	return jen.Qual(dslPkg, dslFunc).Call(
		jen.Id(ifaceName).Dot(methodName),
		callback,
	)
}

func (ctx *migrateCtx) methodLineToJen(line string, info MethodInfo) jen.Code {
	cmd, rest := splitFirst(line)

	if cmd == "map" {
		return ctx.mapLineToJen(rest, info)
	}
	if cmd == "ignore" {
		var args []jen.Code
		for _, f := range strings.Fields(rest) {
			args = append(args, jen.Id("m").Dot("To").Dot(f))
		}
		return jen.Id("m").Dot("Ignore").Call(args...)
	}
	if dslName, ok := dsl.FlagByComment()[cmd]; ok {
		enabled := rest != "no"
		return jen.Id("m").Dot(dslName).Call(jen.Lit(enabled))
	}

	// Typed enum options — need special handling
	if cmd == "enum:unknown" {
		return enumUnknownToJenWithPkg("m.", rest, info.TargetPkg)
	}
	if cmd == "enum:transform" {
		return jen.Id("m").Dot("EnumTransform").Call(enumTransformerToJen(rest))
	}

	methodDefs := methodDefsReverse()
	if named, ok := methodDefs[cmd]; ok {
		fields := strings.Fields(rest)
		var args []jen.Code
		for i, field := range fields {
			kind := named.Def.Args[len(named.Def.Args)-1]
			if i < len(named.Def.Args) {
				kind = named.Def.Args[i]
			}
			switch kind {
			case dsl.AField:
				args = append(args, fieldToJen("From", field))
			case dsl.AStr:
				args = append(args, jen.Lit(field))
			case dsl.ABool:
				args = append(args, jen.Lit(field == "yes" || field == ""))
			case dsl.AFunc:
				args = append(args, ctx.funcRefsToJen(field, "", "")...)
			}
		}
		return jen.Id("m").Dot(named.DSL).Call(args...)
	}

	return jen.Comment("TODO: unsupported method option: " + line)
}

func (ctx *migrateCtx) mapLineToJen(rest string, info MethodInfo) jen.Code {
	parts := strings.SplitN(rest, "|", 2)
	var converter string
	if len(parts) == 2 {
		converter = strings.TrimSpace(parts[1])
	}

	fields := strings.Fields(parts[0])
	var source, target string
	switch len(fields) {
	case 1:
		target = fields[0]
	case 2:
		source = fields[0]
		target = fields[1]
	default:
		return jen.Comment("TODO: cannot parse map: " + rest)
	}

	tgtExpr := fieldToJen("To", target)
	// "map P1" (1-field, no source) means same-name field mapping.
	// Generate m.Map(m.From.P1, m.To.P1) — semantically equivalent.
	srcExpr := fieldToJen("From", source)
	if source == "" && converter == "" {
		srcExpr = fieldToJen("From", target)
	}

	if converter != "" {
		// Pass target type for generic function instantiation
		convExprs := ctx.funcRefsToJen(converter, info.Target, info.TargetPkg)
		convExpr := convExprs[0]
		if source == "" {
			return jen.Id("m").Dot("MapIdentity").Call(tgtExpr, convExpr)
		}
		return jen.Id("m").Dot("MapCustom").Call(srcExpr, tgtExpr, convExpr)
	}
	return jen.Id("m").Dot("Map").Call(srcExpr, tgtExpr)
}

// funcRefsToJen resolves a func ref to jen code.
// targetFieldType is the type of the target field (e.g. "int") for generic instantiation.
// Pass "" if unknown.
func (ctx *migrateCtx) funcRefsToJen(ref, targetFieldType, targetFieldPkg string) []jen.Code {
	pkg, name, err := pkgload.ParseMethodString(ctx.sourcePkg, ref)
	if err != nil {
		return []jen.Code{jen.Id(ref)}
	}

	// Regex patterns
	if strings.ContainsAny(name, "*?[]|+.\\^$(){}") {
		resolved := resolveRegexFuncs(pkg, name, ctx.workDir)
		if len(resolved) > 0 {
			var codes []jen.Code
			for _, fn := range resolved {
				fnName := fn.Name()
				if pkg != ctx.sourcePkg && !token.IsExported(fnName) {
					continue // skip unexported from external packages
				}
				if !isValidExtendFunc(fn) {
					continue // skip consts, wrong arity, etc.
				}
				if pkg == ctx.sourcePkg {
					codes = append(codes, jen.Id(fnName))
				} else {
					codes = append(codes, jen.Qual(pkg, fnName))
				}
			}
			if len(codes) > 0 {
				return codes
			}
		}
		return []jen.Code{jen.Lit(ref)}
	}

	// For external packages: check if we can safely use a Go expression.
	// Fall back to string ref (with FIXME) for:
	// - unexported functions (Go forbids cross-package access)
	// - import cycles (target package imports source package)
	if pkg != ctx.sourcePkg {
		unexported := !token.IsExported(name)
		cycle := ctx.wouldCycle(pkg)
		if unexported || cycle {
			return []jen.Code{jen.Lit(ref)}
		}
	}

	// Check if function is generic — needs type instantiation
	expr := ctx.buildFuncRef(pkg, name)
	if targetFieldType != "" {
		if fn := ctx.lookupFunc(pkg, name); fn != nil {
			sig := fn.Type().(*types.Signature)
			if sig.TypeParams() != nil && sig.TypeParams().Len() > 0 {
				typeArg := typeRefToJen(targetFieldType, targetFieldPkg)
				expr = expr.Types(typeArg)
			}
		}
	}

	return []jen.Code{expr}
}

// wouldCycle checks if importing targetPkg from our source package would
// create an import cycle (targetPkg already imports sourcePkg).
func (ctx *migrateCtx) wouldCycle(targetPkg string) bool {
	pkg := ctx.loadPkg(targetPkg)
	if pkg == nil {
		return false
	}
	// Check if targetPkg's imports include our source package
	for _, imp := range pkg.Imports() {
		if imp.Path() == ctx.sourcePkg {
			return true
		}
	}
	return false
}

func (ctx *migrateCtx) buildFuncRef(pkg, name string) *jen.Statement {
	if pkg == ctx.sourcePkg {
		return jen.Id(name)
	}
	return jen.Qual(pkg, name)
}

func typeRefToJen(name, pkg string) jen.Code {
	if pkg != "" {
		return jen.Qual(pkg, name)
	}
	return jen.Id(name)
}

func fieldToJen(prefix, field string) jen.Code {
	if field == "" || field == "." {
		return jen.Qual(dslPkg, "Source")
	}
	expr := jen.Id("m").Dot(prefix)
	for _, part := range strings.Split(field, ".") {
		expr = expr.Dot(part)
	}
	return expr
}

// enumUnknownToJen generates either EnumUnknown(action) or EnumUnknownConst(value).
// receiver is "" for converter-level, "m." for method-level.
func enumUnknownToJen(receiver, value string) jen.Code {
	return enumUnknownToJenWithPkg(receiver, value, "")
}

func enumUnknownToJenWithPkg(receiver, value, constPkg string) jen.Code {
	if strings.HasPrefix(value, "@") {
		if receiver == "m." {
			return jen.Id("m").Dot("EnumUnknown").Call(enumActionToJen(value))
		}
		return jen.Qual(dslPkg, "EnumUnknown").Call(enumActionToJen(value))
	}
	// Non-action: const name — qualify with package if known
	var constExpr jen.Code
	if constPkg != "" {
		constExpr = jen.Qual(constPkg, value)
	} else {
		constExpr = jen.Id(value)
	}
	if receiver == "m." {
		return jen.Id("m").Dot("EnumUnknownConst").Call(constExpr)
	}
	return jen.Qual(dslPkg, "EnumUnknownConst").Call(constExpr)
}

func enumActionToJen(action string) jen.Code {
	switch action {
	case "@panic":
		return jen.Qual(dslPkg, "EnumPanic")
	case "@error":
		return jen.Qual(dslPkg, "EnumError")
	case "@ignore":
		return jen.Qual(dslPkg, "EnumIgnore")
	default:
		return jen.Lit(action)
	}
}

func enumTransformerToJen(config string) jen.Code {
	parts := strings.SplitN(config, " ", 3)
	if len(parts) >= 3 && parts[0] == "regex" {
		return jen.Qual(dslPkg, "Regex").Call(strLit(parts[1]), strLit(parts[2]))
	}
	// Unknown transformer — pass as string
	return strLit(config)
}

// strLit returns a jen string literal, using a raw string (backtick) when the
// value contains backslashes or double quotes to avoid double-escaping.
func strLit(s string) jen.Code {
	if strings.ContainsAny(s, "\\\"") {
		return jen.Id("`" + s + "`")
	}
	return jen.Lit(s)
}

func splitFirst(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := strings.IndexByte(s, ' ')
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i+1:])
}

func methodDefsReverse() map[string]namedMethodDef {
	m := map[string]namedMethodDef{}
	for dslName, def := range dsl.MethodDefs {
		m[def.Comment] = namedMethodDef{DSL: dslName, Def: def}
	}
	return m
}

type namedMethodDef struct {
	DSL string
	Def dsl.MethodDef
}

func resolveRegexFuncs(pkgPath, pattern, workDir string) []*types.Func {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes, Dir: workDir}
	pkgs, err := packages.Load(cfg, pkgPath)
	if err != nil || len(pkgs) == 0 || pkgs[0].Types == nil {
		return nil
	}
	scope := pkgs[0].Types.Scope()
	var funcs []*types.Func
	for _, name := range scope.Names() {
		loc := re.FindStringIndex(name)
		if len(loc) != 2 || loc[0] != 0 || loc[1] != len(name) {
			continue
		}
		obj := scope.Lookup(name)
		fn, ok := obj.(*types.Func)
		if !ok {
			continue // skip consts, vars, types
		}
		funcs = append(funcs, fn)
	}
	return funcs
}

// isValidExtendFunc returns true if fn is usable as an extend function:
// it must have at least one param and at most one source (non-error, non-result) param.
// We skip functions with multiple ordinary params to avoid the "must have only one source param" error.
func isValidExtendFunc(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	if sig.Params() == nil || sig.Params().Len() == 0 {
		return false
	}
	if sig.Results() == nil || sig.Results().Len() == 0 {
		return false
	}
	// Count non-error params — if more than one, goverter would reject it
	// (unless using context/passArgs, but at regex migration time we can't know)
	params := sig.Params()
	nonErrCount := 0
	for i := 0; i < params.Len(); i++ {
		if !isErrorType(params.At(i).Type()) {
			nonErrCount++
		}
	}
	return nonErrCount == 1
}


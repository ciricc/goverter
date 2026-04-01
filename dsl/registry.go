package dsl

import "strings"

// ArgKind describes what kind of argument a converter option takes.
type ArgKind int

const (
	ArgNone ArgKind = iota // boolean flag, no argument
	ArgStr                 // single string argument
	ArgBool                // bool argument (true/false → yes/no)
	ArgFunc                // function reference(s)
)

// OptDef defines a DSL option and its goverter:comment equivalent.
type OptDef struct {
	DSL     string  // Go function name (e.g. "OutputFile")
	Comment string  // goverter: comment form (e.g. "output:file")
	Arg     ArgKind // what argument type it takes
}

// FlagOpts are boolean options (no arguments) that can appear at
// both converter and method level. Comment form is derived by
// lowercasing the first letter of the DSL name.
var FlagOpts = []string{
	"SkipCopySameType",
	"IgnoreUnexported",
	"WrapErrors",
	"UseUnderlyingTypeMethods",
	"MatchIgnoreCase",
	"IgnoreMissing",
	"UseZeroValueOnPointerInconsistency",
}

// ColonOpts are options with colons in comment form that cannot be
// derived by simple toLowerFirst. Maps DSL name → comment form.
// These can appear at both converter and method level.
var ColonOpts = map[string]string{
	"DefaultUpdate":                      "default:update",
	"UpdateIgnoreZeroValueField":         "update:ignoreZeroValueField",
	"UpdateIgnoreZeroValueFieldBasic":    "update:ignoreZeroValueField:basic",
	"UpdateIgnoreZeroValueFieldStruct":   "update:ignoreZeroValueField:struct",
	"UpdateIgnoreZeroValueFieldNillable": "update:ignoreZeroValueField:nillable",
}

// ConverterOpts are converter-level options that take arguments.
// Comment form cannot be derived automatically (e.g. OutputFile → output:file).
var ConverterOpts = []OptDef{
	{"OutputFile", "output:file", ArgStr},
	{"OutputRaw", "output:raw", ArgStr},
	{"OutputPackage", "output:package", ArgStr},
	{"OutputFormat", "output:format", ArgStr},
	{"Name", "name", ArgStr},
	{"StructComment", "struct:comment", ArgStr},
	{"Extend", "extend", ArgFunc},
	{"WrapErrorsUsing", "wrapErrorsUsing", ArgStr},
	{"Enum", "enum", ArgBool},
}

func toLowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// FlagByDSL returns DSL name → comment form for all flag options.
func FlagByDSL() map[string]string {
	m := make(map[string]string, len(FlagOpts)+len(ColonOpts))
	for _, name := range FlagOpts {
		m[name] = toLowerFirst(name)
	}
	for dslName, comment := range ColonOpts {
		m[dslName] = comment
	}
	return m
}

// FlagByComment returns comment form → DSL name for all flag options.
func FlagByComment() map[string]string {
	m := make(map[string]string, len(FlagOpts)+len(ColonOpts))
	for _, name := range FlagOpts {
		m[toLowerFirst(name)] = name
	}
	for dslName, comment := range ColonOpts {
		m[comment] = dslName
	}
	return m
}

// ConverterOptByDSL returns DSL name → OptDef for converter options with args.
func ConverterOptByDSL() map[string]OptDef {
	m := make(map[string]OptDef, len(ConverterOpts))
	for _, o := range ConverterOpts {
		m[o.DSL] = o
	}
	return m
}

// ConverterOptByComment returns comment form → OptDef for converter options with args.
func ConverterOptByComment() map[string]OptDef {
	m := make(map[string]OptDef, len(ConverterOpts))
	for _, o := range ConverterOpts {
		m[o.Comment] = o
	}
	return m
}

// MethodArgKind describes how to extract an argument from the AST.
type MethodArgKind int

const (
	AField MethodArgKind = iota // struct field path: m.From.X → "X"
	AFunc                       // function reference: strconv.Itoa → "strconv:Itoa"
	AStr                        // string literal: "db" → "db"
	ABool                       // bool literal: true → "yes", false → "no"
)

// MethodDef describes how a Mapping method call translates to a goverter: line.
type MethodDef struct {
	Comment  string          // goverter: command (e.g. "map", "ignore")
	Args     []MethodArgKind // expected argument kinds
	Variadic bool            // last arg kind repeats (e.g. Ignore takes N fields)
	PipeLast bool            // last arg separated by " | " (for converter functions)
}

// MethodDefs maps DSL method names on Mapping to their goverter: line definitions.
// Keys must match actual method names on [Mapping] — validated by registry_test.go.
var MethodDefs = map[string]MethodDef{
	"Map":           {Comment: "map", Args: []MethodArgKind{AField, AField}},
	"MapCustom":     {Comment: "map", Args: []MethodArgKind{AField, AField, AFunc}, PipeLast: true},
	"MapIdentity":   {Comment: "map", Args: []MethodArgKind{AField, AFunc}, PipeLast: true},
	"Ignore":        {Comment: "ignore", Args: []MethodArgKind{AField}, Variadic: true},
	"AutoMap":       {Comment: "autoMap", Args: []MethodArgKind{AField}},
	"Default":       {Comment: "default", Args: []MethodArgKind{AFunc}},
	"Update":        {Comment: "update", Args: []MethodArgKind{AStr}},
	"EnumMap": {Comment: "enum:map", Args: []MethodArgKind{AStr, AStr}},
	"Enum":    {Comment: "enum", Args: []MethodArgKind{ABool}},
}

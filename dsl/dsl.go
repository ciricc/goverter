// Package dsl provides a type-safe, refactor-friendly Go DSL
// for defining goverter converter mappings.
//
// Instead of using goverter: comments on interfaces, you can
// define mappings programmatically with compile-time checked
// references to interfaces, methods, and struct fields.
package dsl

import "regexp"

// Registration holds a converter definition. The variable itself
// is never used at runtime — it exists so that the dsl parser
// can discover it via AST inspection.
type Registration struct{}

// Option is a marker for converter-level and method-level settings.
type Option struct{}

// Conv binds mapping configuration to an interface type.
// The interface is specified as a type parameter — no nil pointer needed.
//
//	var _ = dsl.Conv[MyConverter](
//	    dsl.Method(MyConverter.Convert, func(m *dsl.Mapping[In, Out]) {
//	        m.Map(m.From.FirstName, m.To.Name)
//	    }),
//	)
func Conv[Iface any](opts ...Option) *Registration { return nil }

// --- Converter-level options ---

func OutputFile(path string) Option      { return Option{} }
func OutputRaw(code string) Option       { return Option{} }
func OutputPackage(pkg string) Option    { return Option{} }
func Name(name string) Option            { return Option{} }
func Extend(funcs ...any) Option         { return Option{} }
func ExtendPassArgs(funcs ...any) Option { return Option{} }

// ExtendAllContext registers extend functions where the source parameter is not
// the first one. The contextParams strings explicitly name the context parameters
// so goverter can identify which parameter is the source.
//
//	dsl.ExtendAllContext(DoLookup, "ctx", "ctx2")
//
// Deprecated: This is a compatibility workaround for functions where the source
// parameter is not first. Prefer rewriting the function signature so that the
// source parameter comes first and using [ExtendPassArgs] instead.
func ExtendAllContext(fn any, contextParams ...string) Option   { return Option{} }
func SkipCopySameType(enabled ...bool) Option                   { return Option{} }
func IgnoreUnexported(enabled ...bool) Option                   { return Option{} }
func WrapErrors(enabled ...bool) Option                         { return Option{} }
func WrapErrorsUsing(fn any) Option                             { return Option{} }
func UseUnderlyingTypeMethods(enabled ...bool) Option           { return Option{} }
func MatchIgnoreCase(enabled ...bool) Option                    { return Option{} }
func IgnoreMissing(enabled ...bool) Option                      { return Option{} }
func UseZeroValueOnPointerInconsistency(enabled ...bool) Option { return Option{} }
func DefaultUpdate(enabled ...bool) Option                      { return Option{} }
func UpdateIgnoreZeroValueField(enabled ...bool) Option         { return Option{} }
func UpdateIgnoreZeroValueFieldBasic(enabled ...bool) Option    { return Option{} }
func UpdateIgnoreZeroValueFieldStruct(enabled ...bool) Option   { return Option{} }
func UpdateIgnoreZeroValueFieldNillable(enabled ...bool) Option { return Option{} }
func Enum(enabled bool) Option                                  { return Option{} }
func EnumUnknown(action EnumAction) Option                      { return Option{} }
func EnumUnknownConst(value any) Option                         { return Option{} }
func EnumExclude(pattern *regexp.Regexp) Option                 { return Option{} }
func StructComment(comment string) Option                       { return Option{} }
func OutputFormat(format string) Option                         { return Option{} }

// EnumAction represents an action for unmapped enum values.
type EnumAction struct{ name string }

var (
	// EnumPanic generates a panic for unmapped enum values.
	EnumPanic = EnumAction{"@panic"}
	// EnumError generates an error return for unmapped enum values.
	EnumError = EnumAction{"@error"}
	// EnumIgnore skips unmapped enum values.
	EnumIgnore = EnumAction{"@ignore"}
)

// EnumTransformer represents a strategy for transforming enum member names.
type EnumTransformer struct {
	name   string
	config string
}

// Regex creates an enum transformer that applies a regex replacement
// to source enum names to derive target names.
//
//	m.EnumTransform(dsl.Regex(`Color(\w+)`, `$1`))
func Regex(pattern, replacement string) EnumTransformer {
	return EnumTransformer{name: "regex", config: pattern + " " + replacement}
}

// Source is a sentinel value referencing the entire source object
// (equivalent to "." in goverter:map).
//
//	m.MapCustom(dsl.Source, m.To.FullName, GetFullName)
var Source any

// Mapping holds phantom source and target type instances.
// Access fields directly through m.From and m.To — the compiler
// validates that referenced fields exist on the actual types.
type Mapping[From, To any] struct {
	From From
	To   To
}

func (m *Mapping[From, To]) Map(source, target any)                             {}
func (m *Mapping[From, To]) MapCustom(source, target, conv any)                 {}
func (m *Mapping[From, To]) MapIdentity(target, conv any)                       {}
func (m *Mapping[From, To]) Ignore(fields ...any)                               {}
func (m *Mapping[From, To]) AutoMap(source any)                                 {}
func (m *Mapping[From, To]) Default(constructor any)                            {}
func (m *Mapping[From, To]) Update(paramName string)                            {}
func (m *Mapping[From, To]) IgnoreMissing(enabled ...bool)                      {}
func (m *Mapping[From, To]) IgnoreUnexported(enabled ...bool)                   {}
func (m *Mapping[From, To]) SkipCopySameType(enabled ...bool)                   {}
func (m *Mapping[From, To]) WrapErrors(enabled ...bool)                         {}
func (m *Mapping[From, To]) MatchIgnoreCase(enabled ...bool)                    {}
func (m *Mapping[From, To]) UseZeroValueOnPointerInconsistency(enabled ...bool) {}
func (m *Mapping[From, To]) UseUnderlyingTypeMethods(enabled ...bool)           {}
func (m *Mapping[From, To]) DefaultUpdate(enabled ...bool)                      {}
func (m *Mapping[From, To]) UpdateIgnoreZeroValueField(enabled ...bool)         {}
func (m *Mapping[From, To]) UpdateIgnoreZeroValueFieldBasic(enabled ...bool)    {}
func (m *Mapping[From, To]) UpdateIgnoreZeroValueFieldStruct(enabled ...bool)   {}
func (m *Mapping[From, To]) UpdateIgnoreZeroValueFieldNillable(enabled ...bool) {}

// EnumMap maps a source enum const to a target enum const or action.
// Both arguments must be const references or enum actions.
//
//	m.EnumMap(input.StatusActive, output.Active)
//	m.EnumMap(input.StatusUnknown, dsl.EnumIgnore)
func (m *Mapping[From, To]) EnumMap(source, target any) {}

// EnumTransform applies a transformer to generate enum name mappings.
//
//	m.EnumTransform(dsl.Regex(`Color(\w+)`, `$1`))
func (m *Mapping[From, To]) EnumTransform(t EnumTransformer) {}

// EnumUnknown sets the action for unmapped source enum values.
//
//	m.EnumUnknown(dsl.EnumPanic)
func (m *Mapping[From, To]) EnumUnknown(action EnumAction) {}

// EnumUnknownConst sets a default const value for unmapped source enum values.
//
//	m.EnumUnknownConst(output.Unknown)
func (m *Mapping[From, To]) EnumUnknownConst(value any) {}

// Enum enables or disables enum conversion for this method.
func (m *Mapping[From, To]) Enum(enabled bool) {}

// Method configures mapping for a method where all parameters are source or target.
// Extra parameters beyond source will cause an error — use [MethodPassArgs]
// for methods with additional pass-through arguments.
//
//	dsl.Method(MyConverter.Convert, func(m *dsl.Mapping[Input, Output]) {
//	    m.Map(m.From.FirstName, m.To.Name)
//	})
func Method[From, To any](method any, configure func(m *Mapping[From, To])) Option {
	return Option{}
}

// MethodAuto registers a method with no additional mapping configuration.
// Equivalent to Method with an empty configure function, but without requiring
// explicit type parameters.
//
//	dsl.MethodAuto(MyConverter.Convert)
func MethodAuto(method any) Option { return Option{} }

// MethodAutoPassArgs is like [MethodAuto] but for methods with extra pass-through arguments.
//
//	dsl.MethodAutoPassArgs(MyConverter.Convert)
func MethodAutoPassArgs(method any) Option { return Option{} }

// MethodPassArgs configures mapping for a method that has extra arguments
// beyond the source. All parameters except the first (source), update target,
// and self-interface are automatically passed through to sub-converters.
//
//	dsl.MethodPassArgs(MyConverter.Convert, func(m *dsl.Mapping[Input, Output]) {
//	    m.Map(m.From.FirstName, m.To.Name)
//	})
func MethodPassArgs[From, To any](method any, configure func(m *Mapping[From, To])) Option {
	return Option{}
}

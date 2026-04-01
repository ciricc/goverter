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

// OutputFile sets the path of the generated file, relative to the current source file.
//
//	dsl.OutputFile("./generated/generated.go")
func OutputFile(path string) Option { return Option{} }

// OutputRaw appends a raw Go code string to the generated file.
// Useful for adding build constraints or custom declarations.
//
//	dsl.OutputRaw("//go:build !integration")
func OutputRaw(code string) Option { return Option{} }

// OutputPackage sets the import path of the package where the generated file will be placed.
// Use together with [OutputFile] when the output package differs from the converter package.
//
//	dsl.OutputPackage("example.com/pkg/generated")
func OutputPackage(pkg string) Option { return Option{} }

// Name overrides the name of the generated converter struct.
// By default goverter uses the interface name with "Impl" suffix.
//
//	dsl.Name("MyConverterImpl")
func Name(name string) Option { return Option{} }

// Extend registers one or more custom conversion functions that goverter will use
// when it needs to convert between specific types. Each function must have a
// signature compatible with goverter's extend rules.
//
//	dsl.Extend(FormatID, ParseDate)
func Extend(funcs ...any) Option { return Option{} }

// ExtendPassArgs registers extend functions where all non-source parameters
// are automatically passed through from the calling method's extra arguments.
//
//	dsl.ExtendPassArgs(LookupUser)
func ExtendPassArgs(funcs ...any) Option { return Option{} }

// ExtendPkg registers all exported functions from the package containing symbol
// whose names match pattern (a Go regexp). If pattern is omitted, all exported
// functions are included. Any exported symbol from the target package can be used.
//
//	dsl.ExtendPkg(mypkg.AnyExportedSymbol)
//	dsl.ExtendPkg(mypkg.AnyExportedSymbol, regexp.MustCompile("Convert.*"))
func ExtendPkg(symbol any, pattern ...*regexp.Regexp) Option { return Option{} }

// ExtendAllContext registers extend functions where the source parameter is not
// the first one. The contextParams strings explicitly name the context parameters
// so goverter can identify which parameter is the source.
//
//	dsl.ExtendAllContext(DoLookup, "ctx", "ctx2")
//
// Deprecated: This is a compatibility workaround for functions where the source
// parameter is not first. Prefer rewriting the function signature so that the
// source parameter comes first and using [ExtendPassArgs] instead.
func ExtendAllContext(fn any, contextParams ...string) Option { return Option{} }

// SkipCopySameType disables copying when source and target types are identical.
// Useful to avoid unnecessary allocations for large structs.
//
//	dsl.SkipCopySameType()        // enable (default: false)
//	dsl.SkipCopySameType(false)   // disable explicitly
func SkipCopySameType(enabled ...bool) Option { return Option{} }

// IgnoreUnexported silently skips unexported fields on the target struct
// instead of returning an error.
//
//	dsl.IgnoreUnexported()
func IgnoreUnexported(enabled ...bool) Option { return Option{} }

// WrapErrors wraps conversion errors with additional context about the field path.
//
//	dsl.WrapErrors()
func WrapErrors(enabled ...bool) Option { return Option{} }

// WrapErrorsUsing sets a custom error wrapping package used when [WrapErrors] is enabled.
// Pass the Wrap function from your error package — goverter will use the entire package
// (Wrap, Field, and optionally Index, Key).
// Cannot be used together with [WrapErrors].
//
// The package must implement this contract (no ready-made libraries exist — write your own):
//
//	type Field any   // can be any type
//	func Wrap(err error, fields ...Field) error
//	func Field(name string) Field
//	func Index(i int) Field  // optional, for slice element errors
//	func Key(k any) Field    // optional, for map key errors
//
//	dsl.WrapErrorsUsing(werror.Wrap)
func WrapErrorsUsing[F any](wrapFn func(error, ...F) error) Option { return Option{} }

// UseUnderlyingTypeMethods allows goverter to use methods defined on the underlying
// type of a named type when converting.
//
//	dsl.UseUnderlyingTypeMethods()
func UseUnderlyingTypeMethods(enabled ...bool) Option { return Option{} }

// MatchIgnoreCase matches source and target fields case-insensitively.
// Useful when naming conventions differ between source and target.
//
//	dsl.MatchIgnoreCase()
func MatchIgnoreCase(enabled ...bool) Option { return Option{} }

// IgnoreMissing silently ignores target fields that have no matching source field
// instead of returning a compile-time error.
//
//	dsl.IgnoreMissing()
func IgnoreMissing(enabled ...bool) Option { return Option{} }

// UseZeroValueOnPointerInconsistency uses the zero value when a pointer source is nil
// and the target is a non-pointer (instead of returning an error).
//
//	dsl.UseZeroValueOnPointerInconsistency()
func UseZeroValueOnPointerInconsistency(enabled ...bool) Option { return Option{} }

// DefaultUpdate makes all methods behave as update methods by default —
// the first parameter is treated as the value to update rather than a source to copy from.
//
//	dsl.DefaultUpdate()
func DefaultUpdate(enabled ...bool) Option { return Option{} }

// UpdateIgnoreZeroValueField skips updating target fields when the source value is
// the zero value for its type (applies to all field kinds).
//
//	dsl.UpdateIgnoreZeroValueField()
func UpdateIgnoreZeroValueField(enabled ...bool) Option { return Option{} }

// UpdateIgnoreZeroValueFieldBasic is like [UpdateIgnoreZeroValueField] but only
// applies to basic types (int, string, bool, etc.).
//
//	dsl.UpdateIgnoreZeroValueFieldBasic()
func UpdateIgnoreZeroValueFieldBasic(enabled ...bool) Option { return Option{} }

// UpdateIgnoreZeroValueFieldStruct is like [UpdateIgnoreZeroValueField] but only
// applies to struct types.
//
//	dsl.UpdateIgnoreZeroValueFieldStruct()
func UpdateIgnoreZeroValueFieldStruct(enabled ...bool) Option { return Option{} }

// UpdateIgnoreZeroValueFieldNillable is like [UpdateIgnoreZeroValueField] but only
// applies to nillable types (pointers, slices, maps, interfaces).
//
//	dsl.UpdateIgnoreZeroValueFieldNillable()
func UpdateIgnoreZeroValueFieldNillable(enabled ...bool) Option { return Option{} }

// Enum enables enum conversion mode for all methods in this converter.
// When enabled, goverter validates that all source enum values are explicitly mapped.
//
//	dsl.Enum(true)
func Enum(enabled bool) Option { return Option{} }

// EnumUnknown sets the action to take when a source enum value has no mapping.
// Use [EnumPanic], [EnumError], or [EnumIgnore].
//
//	dsl.EnumUnknown(dsl.EnumError)
func EnumUnknown(action EnumAction) Option { return Option{} }

// EnumUnknownConst sets a specific target const value to use for unmapped source enum values.
//
//	dsl.EnumUnknownConst(output.StatusUnknown)
func EnumUnknownConst(value any) Option { return Option{} }

// EnumExclude excludes source enum values matching the pattern from mapping validation.
// Excluded values are silently ignored even if unmapped.
//
//	dsl.EnumExclude(regexp.MustCompile(`^Internal.*`))
func EnumExclude(pattern *regexp.Regexp) Option { return Option{} }

// StructComment sets a Go doc comment on the generated converter struct.
//
//	dsl.StructComment("ConverterImpl converts between domain and API types.")
func StructComment(comment string) Option { return Option{} }

// OutputFormatValue represents the output format of the generated converter.
type OutputFormatValue string

const (
	// OutputFormatStruct generates a struct with methods (default).
	OutputFormatStruct OutputFormatValue = "struct"
	// OutputFormatAssignVariable generates an assign-variable style converter.
	OutputFormatAssignVariable OutputFormatValue = "assign-variable"
	// OutputFormatFunction generates standalone functions instead of methods.
	OutputFormatFunction OutputFormatValue = "function"
)

// OutputFormat sets the format of the generated converter output.
//
//	dsl.OutputFormat(dsl.OutputFormatFunction)
func OutputFormat(format OutputFormatValue) Option { return Option{} }

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

// Map maps a source field to a target field.
// Use m.From and m.To to reference fields — the compiler validates they exist.
//
//	m.Map(m.From.Nested.Name, m.To.FullName)
//	m.Map(dsl.Source, m.To.Self)  // map entire source object
func (m *Mapping[From, To]) Map(source, target any) {}

// MapCustom maps a source field to a target field using a custom converter function.
// The converter function must accept the source field type and return the target field type.
//
//	m.MapCustom(m.From.RawID, m.To.ID, ParseID)
func (m *Mapping[From, To]) MapCustom(source, target, conv any) {}

// Set maps a target field to the return value of fn.
// fn must be a no-argument function returning the target field type: func() FieldType.
// Use this for computed, generated, or constant fields where no source value is needed.
//
//	m.Set(m.To.CreatedAt, time.Now)
//	m.Set(m.To.Version, getVersion)
func (m *Mapping[From, To]) Set(target, fn any) {}

// Ignore marks one or more target fields as intentionally unmapped.
// Without this, goverter returns an error for any unmapped target field.
//
//	m.Ignore(m.To.InternalID, m.To.CreatedAt)
func (m *Mapping[From, To]) Ignore(fields ...any) {}

// AutoMap instructs goverter to automatically map all fields from a nested source struct
// as if they were top-level fields on the source.
//
//	m.AutoMap(m.From.Address)
func (m *Mapping[From, To]) AutoMap(source any) {}

// Default sets a constructor function used to initialize the target value.
// The function must return the target type or a pointer to it.
//
//	m.Default(NewOutput)
func (m *Mapping[From, To]) Default(constructor any) {}

// Update marks a method parameter (by name) as the value to update in-place,
// rather than creating a new target value.
//
//	m.Update("existing")
func (m *Mapping[From, To]) Update(paramName string) {}

// IgnoreMissing silently ignores target fields with no matching source field
// for this method only. See also converter-level [IgnoreMissing].
func (m *Mapping[From, To]) IgnoreMissing(enabled ...bool) {}

// IgnoreUnexported silently skips unexported target fields for this method only.
// See also converter-level [IgnoreUnexported].
func (m *Mapping[From, To]) IgnoreUnexported(enabled ...bool) {}

// SkipCopySameType disables copying when source and target types are identical
// for this method only. See also converter-level [SkipCopySameType].
func (m *Mapping[From, To]) SkipCopySameType(enabled ...bool) {}

// WrapErrors wraps conversion errors with field path context for this method only.
// See also converter-level [WrapErrors].
func (m *Mapping[From, To]) WrapErrors(enabled ...bool) {}

// MatchIgnoreCase matches fields case-insensitively for this method only.
// See also converter-level [MatchIgnoreCase].
func (m *Mapping[From, To]) MatchIgnoreCase(enabled ...bool) {}

// UseZeroValueOnPointerInconsistency uses zero value for nil pointer sources
// for this method only. See also converter-level [UseZeroValueOnPointerInconsistency].
func (m *Mapping[From, To]) UseZeroValueOnPointerInconsistency(enabled ...bool) {}

// UseUnderlyingTypeMethods allows methods on the underlying type for this method only.
// See also converter-level [UseUnderlyingTypeMethods].
func (m *Mapping[From, To]) UseUnderlyingTypeMethods(enabled ...bool) {}

// DefaultUpdate makes this method behave as an update method.
// See also converter-level [DefaultUpdate].
func (m *Mapping[From, To]) DefaultUpdate(enabled ...bool) {}

// UpdateIgnoreZeroValueField skips updating fields when the source is zero for this method.
// See also converter-level [UpdateIgnoreZeroValueField].
func (m *Mapping[From, To]) UpdateIgnoreZeroValueField(enabled ...bool) {}

// UpdateIgnoreZeroValueFieldBasic skips updating basic-type fields when source is zero.
// See also converter-level [UpdateIgnoreZeroValueFieldBasic].
func (m *Mapping[From, To]) UpdateIgnoreZeroValueFieldBasic(enabled ...bool) {}

// UpdateIgnoreZeroValueFieldStruct skips updating struct fields when source is zero.
// See also converter-level [UpdateIgnoreZeroValueFieldStruct].
func (m *Mapping[From, To]) UpdateIgnoreZeroValueFieldStruct(enabled ...bool) {}

// UpdateIgnoreZeroValueFieldNillable skips updating nillable fields when source is zero.
// See also converter-level [UpdateIgnoreZeroValueFieldNillable].
func (m *Mapping[From, To]) UpdateIgnoreZeroValueFieldNillable(enabled ...bool) {}

// EnumMap maps a source enum const to a target enum const or action.
// Both arguments must be const references or enum actions.
//
//	m.EnumMap(input.StatusActive, output.Active)
//	m.EnumMap(input.StatusUnknown, dsl.EnumIgnore)
func (m *Mapping[From, To]) EnumMap(source, target any) {}

// EnumTransform applies a transformer to generate enum name mappings automatically.
//
//	m.EnumTransform(dsl.Regex(`Color(\w+)`, `$1`))
func (m *Mapping[From, To]) EnumTransform(t EnumTransformer) {}

// EnumUnknown sets the action for unmapped source enum values for this method only.
//
//	m.EnumUnknown(dsl.EnumPanic)
func (m *Mapping[From, To]) EnumUnknown(action EnumAction) {}

// EnumUnknownConst sets a default const value for unmapped source enum values for this method only.
//
//	m.EnumUnknownConst(output.Unknown)
func (m *Mapping[From, To]) EnumUnknownConst(value any) {}

// Enum enables or disables enum conversion for this method only.
// See also converter-level [Enum].
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

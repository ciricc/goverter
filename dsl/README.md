# goverter DSL

A type-safe, refactor-friendly Go API for defining goverter converter mappings — an alternative to `// goverter:` comments on interfaces.

> **Status**: experimental. API may change before stable release.

## Why DSL?

| | Comments | DSL |
|---|---|---|
| Compiler checks | No | Yes |
| IDE rename/refactor | No | Yes |
| Go to definition | No | Yes |
| Readable diffs | Yes | Yes |

## Installation

Requires Go 1.23+.

```bash
go get github.com/jmattheis/goverter
```

## Quick start

**Before** (comment-based):

```go
package example

// goverter:converter
// goverter:output:file ./generated/generated.go
type Converter interface {
    // goverter:ignore Irrelevant
    // goverter:map Nested.AgeInYears Age
    Convert(source Input) Output
}
```

**After** (DSL):

```go
package example

import "github.com/jmattheis/goverter/dsl"

var _ = dsl.Conv[Converter](
    dsl.OutputFile("./generated/generated.go"),
    dsl.Method(Converter.Convert, func(m *dsl.Mapping[Input, Output]) {
        m.Ignore(m.To.Irrelevant)
        m.Map(m.From.Nested.AgeInYears, m.To.Age)
    }),
)
```

Both approaches can coexist — DSL is parsed alongside comment-based converters.

## Converter options

```go
var _ = dsl.Conv[MyConverter](
    dsl.OutputFile("./generated/generated.go"), // output file path (relative to this file)
    dsl.OutputPackage("example.com/pkg/gen"),   // output package import path
    dsl.Name("CustomConverterImpl"),             // override generated struct name
    dsl.Extend(helperFunc, anotherHelper),       // register custom conversion functions
    dsl.ExtendPassArgs(helperWithCtx),           // extend where all non-source params are passed through
    dsl.WrapErrors(true),                        // wrap errors with context
    dsl.IgnoreMissing(true),                     // ignore unmapped target fields
    dsl.MatchIgnoreCase(true),                   // match fields case-insensitively
)
```

## Method configuration

### Basic mapping

```go
dsl.Method(Converter.Convert, func(m *dsl.Mapping[Input, Output]) {
    m.Map(m.From.FirstName, m.To.Name)          // map field to field
    m.Ignore(m.To.Irrelevant, m.To.Internal)    // ignore fields
    m.MapCustom(m.From.ID, m.To.ExternalID, formatID) // map with custom converter
})
```

### No configuration needed

```go
dsl.MethodAuto(Converter.ConvertItem)
```

### Methods with extra pass-through arguments

```go
// All parameters except source are passed through to sub-converters.
dsl.MethodPassArgs(Converter.Convert, func(m *dsl.Mapping[Input, Output]) {
    m.Map(m.From.Name, m.To.FullName)
})

// No configuration:
dsl.MethodAutoPassArgs(Converter.Convert)
```

## Field mapping

```go
m.Map(m.From.Nested.Field, m.To.Target)        // nested source field
m.Map(dsl.Source, m.To.Target)                  // entire source object as field
m.MapCustom(m.From.Field, m.To.Target, convert) // with custom converter function
m.MapIdentity(m.To.Target, convert)             // identity mapping with converter
m.AutoMap(m.From.Nested)                        // auto-map all fields from nested struct
m.Ignore(m.To.Field1, m.To.Field2)             // ignore multiple fields
m.Default(NewOutput)                            // default constructor for output
m.Update("paramName")                           // update existing value via parameter
```

## Enum mapping

```go
dsl.Method(Converter.ConvertStatus, func(m *dsl.Mapping[InputStatus, OutputStatus]) {
    m.Enum(true)
    m.EnumMap(InputActive, OutputEnabled)
    m.EnumUnknown(dsl.EnumError)
    m.EnumTransform(dsl.Regex(`Status(\w+)`, `$1`))
})
```

Converter-level enum defaults:

```go
dsl.Conv[MyConverter](
    dsl.Enum(true),
    dsl.EnumUnknown(dsl.EnumPanic),
    dsl.EnumExclude(regexp.MustCompile(`^Internal.*`)),
)
```

## Migrating from comments

Install the migration tool and the patched goverter (uses a separate binary name to avoid overwriting your existing `goverter`):

```bash
git clone https://github.com/ciricc/goverter
cd goverter

# installs as "goverter-migrate" — does not conflict with your existing goverter binary
go install ./cmd/goverter-migrate

# installs patched goverter as "goverter-dsl" 
GOBIN=$(go env GOPATH)/bin go build -o $(go env GOPATH)/bin/goverter-dsl ./cmd/goverter
```

Run migration:

```bash
# Preview changes without writing files
goverter-migrate -dry-run ./...

# Apply migration
goverter-migrate ./...
```

Then use `goverter-dsl` instead of `goverter` to generate converters:

```bash
goverter-dsl gen ./...
```

The migration tool:
- Generates `goverter_dsl.go` next to each converter package
- Removes `// goverter:` comments from original files
- Warns about `goverter:variables` converters (not supported by DSL)

## Reference

All available options mirror the comment-based API. See the [goverter settings reference](https://goverter.jmattheis.de/reference/settings) for full documentation of each option.

# goverter DSL

A type-safe, refactor-friendly Go API for defining goverter converter mappings — an alternative to `// goverter:` comments on interfaces.

> **Status**: experimental. API may change before stable release.

## Why DSL?

|                     | Comments | DSL |
| ------------------- | -------- | --- |
| Compiler checks     | No       | Yes |
| IDE rename/refactor | No       | Yes |
| Go to definition    | No       | Yes |
| Readable diffs      | Yes      | Yes |

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

Since this fork keeps the original module path (`github.com/jmattheis/goverter`), add a `replace` directive to your `go.mod`.

Get the latest pseudo-version:

```bash
go list -m github.com/ciricc/goverter@main
```

Then add to `go.mod`:

```
replace github.com/jmattheis/goverter => github.com/ciricc/goverter <version>
```

Or in one command:

```bash
GOPROXY=direct go mod edit -replace github.com/jmattheis/goverter=github.com/ciricc/goverter@<version>
go mod tidy
```

---

Clone the fork and install the tools:

```bash
git clone https://github.com/ciricc/goverter
cd goverter

# goverter-migrate — migration tool (won't overwrite your existing goverter)
go install ./cmd/goverter-migrate

# goverter-dsl — patched goverter that understands DSL
go build -o $(go env GOPATH)/bin/goverter-dsl ./cmd/goverter
```

Run migration:

```bash
# Preview changes without writing files
goverter-migrate -dry-run ./...

# Apply migration
goverter-migrate ./...
```

The tool automatically finds all packages containing `goverter:converter` comments — no need to specify them manually, `./...` works even in large projects with generated code.

Then use `goverter-dsl` instead of `goverter` to generate converters from DSL:

```bash
goverter-dsl gen ./...
```

The migration tool:
- Generates `goverter_dsl.go` next to each converter package
- Removes `// goverter:` comments from original files
- Skips unrelated packages to avoid build errors in code that imports generated files
- Warns about `goverter:variables` converters (not supported by DSL)

## Comparison with convgen

[convgen](https://github.com/sublee/convgen) is a similar tool with a comparable DSL-first approach. Here's how they differ:

**Declaration style**

```go
// convgen — standalone function per conversion
var ConvertUser = convgen.Struct[User, api.User](nil,
    convgen.Match(User{}.Name, api.User{}.Username),
)

// goverter DSL — methods grouped under a converter interface
var _ = dsl.Conv[UserConverter](
    dsl.Method(UserConverter.Convert, func(m *dsl.Mapping[User, api.User]) {
        m.Map(m.From.Name, m.To.Username)
    }),
)
```

**Feature comparison**

|                              | convgen       | goverter DSL                              |
| ---------------------------- | ------------- | ----------------------------------------- |
| Output                       | Functions     | Struct with methods                       |
| DI / interface mocking       | Manual        | Built-in                                  |
| Multiple conversions         | Separate vars | One `Conv[T]` block                       |
| Union / interface types      | Yes           | No                                        |
| Error wrapping               | No            | Yes                                       |
| Drop-in for existing project | No            | Yes — same engine, same generated code    |
| Migration tool               | No            | `goverter-migrate`                        |
| Golangci-lint plugin         | Yes           | No                                        |
| Maturity                     | Early stage   | Experimental fork — no stable release yet |

**When to choose goverter DSL**: you already use goverter, want maximum automation (pointers, slices, maps, nested paths handled automatically), or need a drop-in replacement for comment-based definitions — same generator, same output, no migration risk.

**When to choose convgen**: you prefer a minimal explicit approach where complex conversions are always custom functions, need union/interface type conversions, or want golangci-lint integration.

## Known limitations

- **`dsl.Method` type safety**: the `Mapping[From, To]` type parameters are not verified against the actual method signature on the converter interface. A mismatch compiles silently and is only caught at `goverter-dsl gen` time. Full compile-time enforcement is not possible in Go for methods that return `(T, error)` or accept extra pass-through parameters.

## TODO

- [ ] Support variable-based converters (`goverter:variables`)
- [ ] Split code into focused responsibility zones (parse / migrate / generate)
- [ ] Write unit tests for DSL-specific logic
- [ ] Refactor and reduce overall complexity
- [ ] golangci-lint plugin

## Reference

All available options mirror the comment-based API. See the [goverter settings reference](https://goverter.jmattheis.de/reference/settings) for full documentation of each option.

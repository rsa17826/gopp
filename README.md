# pp

A colorized, recursive pretty-printer for Go, modeled after a Python
`print` helper class that formats values with ANSI color and wraps
collections onto multiple lines once they get too wide.

## Usage

```go
p := pp.New()
p.Plain("hello", 42, map[string]any{"a": 1, "b": []int{1, 2, 3}})
p.Debug("starting up", cfg)   // gated by p.ShowDebug (default: on)
p.Info("listening on", 8080)  // gated by p.ShowInfo  (default: off)
p.Warn("cache miss for", key)
p.Error("request failed:", err)
p.Success("deployment complete")



type User struct {
	Name  string
	Age   int
	Tags  []string
	Score float64
}

func greet(name string) string { return "hi " + name }

func main() {
	p := pp.New()
	p.ShowInfo = true

	p.Plain("hello world", 42, 1234567.5, true, false, nil)

	p.Plain(map[string]any{
		"id":   1,
		"name": "Ada Lovelace",
		"tags": []string{"math", "computing"},
	})

	p.Plain(User{
		Name:  "Grace Hopper",
		Age:   85,
		Tags:  []string{"navy", "compilers", "cobol", "debugging", "mathematics", "programming"},
		Score: 1234567.891,
	})

	p.Plain([]int{1, 2, 3})
	p.Plain(greet)

	p.Debug("cache warmed in", 123, "ms")
	p.Info("listening on port", 8080)
	p.Warn("disk usage high:", 91.5, "%")
	p.Error("request failed:", errors.New("connection refused"))
	p.Success("deployment complete")
}

```

Or use the package-level `pp.Plain(...)`, `pp.Debug(...)`, etc., which
operate on a shared `pp.Default` printer.

`FormatItem` gives you the formatted string without printing it, if you
want to build your own log lines:

```go
line := p.FormatItem(someStruct)
```

## Behavior

- **Strings** — quoted and colored purple, with `\` and `"` escaped.
- **Numbers** (any int/uint/float kind) — colored green, with thousands
  separators inserted into the integer part (`1234567` → `1,234,567`,
  `1234.5` → `1,234.5`). Decimal digits are left exactly as-is, no rounding.
- **Bools / nil** — `true` / `false` (uncolored) and `null` (orange).
- **error** — rendered as its `.Error()` message, not its internal struct.
- **fmt.Stringer** — rendered via `.String()`.
- **Maps and structs** — rendered as `{ "k": v, "k": v }` on one line if it
  fits within `WrapAt` (default 80) visible characters, otherwise wraps to
  one `"k": v` pair per line, indented. Struct field names are used as
  keys; unexported fields are skipped. Map keys are sorted for
  deterministic output (Go maps have no defined iteration order, unlike
  Python dicts).
- **Slices/arrays** — same idea, `[ a, b, c ]` or one-per-line.
- **Named types** — get a `╟TypeName╣` label before the value.
- **Funcs** — rendered as `<function Name>`.

## Config

```go
p := pp.New()
p.NoColor = true    // disable ANSI color entirely
p.ShowDebug = false // silence p.Debug()
p.ShowInfo = true   // enable p.Info() (off by default)
p.WrapAt = 100       // widen the single-line/multi-line threshold
p.Out = someWriter   // default os.Stdout
```

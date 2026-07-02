// Package pp is a colorized, recursive pretty-printer for Go values, modeled
// after a Python `print` helper class. It renders strings, numbers, bools,
// maps, slices/arrays, structs, and funcs with ANSI color, wrapping
// collections onto multiple lines once they get too wide (like Python's
// pprint, but with color and a JS/JSON-ish syntax).
//
// Quick start:
//
//	p := pp.New()
//	p.Plain("hello", 42, map[string]any{"a": 1, "b": []int{1, 2, 3}})
//	p.Debug("starting up", cfg)
//	p.Warn("cache miss for", key)
//	p.Error("request failed:", err)
package pp

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Colors
// ---------------------------------------------------------------------------

// colorCodes maps color names (case-insensitive) to ANSI escape sequences.
// Both plain color names ("red") and the SHOUTY variants used for labels
// ("RED") resolve to the same code — lookups are always lower-cased.
var colorCodes = map[string]string{
	"red":         "\033[31m",
	"green":       "\033[32m",
	"yellow":      "\033[33m",
	"blue":        "\033[34m",
	"purple":      "\033[35m",
	"cyan":        "\033[36m",
	"pink":        "\033[95m",
	"orange":      "\033[38;5;208m",
	"bright blue": "\033[94m",
	"bold":        "\033[1m",
	"end":         "\033[0m",
}

// ansiRe strips ANSI escape sequences, used to measure the *visible* width
// of an already-colored string when deciding whether to wrap.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// ---------------------------------------------------------------------------
// Printer
// ---------------------------------------------------------------------------

// Printer holds configuration for pretty-printing, mirroring the Python
// `print` class's mutable class attributes (getcolor, showdebug, showinfo).
type Printer struct {
	// NoColor disables ANSI color codes entirely (the Python `--pp` flag).
	NoColor bool
	// ShowDebug gates Debug(); defaults to true.
	ShowDebug bool
	// ShowInfo gates Info(); defaults to false.
	ShowInfo bool
	// Out is where output is written; defaults to os.Stdout.
	Out io.Writer
	// WrapAt is the visible-width threshold past which maps/slices/structs
	// switch from a single-line to a multi-line rendering. Defaults to 80.
	WrapAt int
}

// New returns a Printer with the same defaults as the Python class:
// debug logs shown, info logs hidden, color enabled.
func New() *Printer {
	return &Printer{
		ShowDebug: true,
		ShowInfo:  false,
		Out:       os.Stdout,
		WrapAt:    80,
	}
}

// Default is a ready-to-use Printer, analogous to using the Python `print`
// class directly rather than instantiating it.
var Default = New()

func (p *Printer) wrapAt() int {
	if p.WrapAt <= 0 {
		return 80
	}
	return p.WrapAt
}

func (p *Printer) out() io.Writer {
	if p.Out == nil {
		return os.Stdout
	}
	return p.Out
}

func (p *Printer) color(name string) string {
	if p.NoColor {
		return ""
	}
	return colorCodes[strings.ToLower(name)]
}

// ---------------------------------------------------------------------------
// Output methods (mirrors print.plain / .debug / .info / .warn / .error / .success)
// ---------------------------------------------------------------------------

type writeOpts struct {
	sep string
	end string
}

func (p *Printer) writeParts(parts []string, o writeOpts) {
	fmt.Fprint(p.out(), strings.Join(parts, o.sep)+o.end)
}

// Plain prints each argument formatted and colorized, with no level label.
func (p *Printer) Log(a ...any) {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = p.FormatItem(v, false)
	}
	p.writeParts(parts, writeOpts{sep: " ", end: "\n"})
}
func (p *Printer) Plain(a ...any) {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = p.FormatItem(v, false)
	}
	p.writeParts(parts, writeOpts{sep: " ", end: "\n"})
}

func (p *Printer) Plainest(a ...any) {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = p.FormatItem(v, true)
	}
	p.writeParts(parts, writeOpts{sep: " ", end: "\n"})
}

// Debug prints with a "[DEBUG]" label, if ShowDebug is enabled.
func (p *Printer) Debug(a ...any) {
	if !p.ShowDebug {
		return
	}
	p.printLabeled("DEBUG", "blue", a...)
}

// Info prints with an "[INFO]" label, if ShowInfo is enabled.
func (p *Printer) Info(a ...any) {
	if !p.ShowInfo {
		return
	}
	p.printLabeled("INFO", "bright blue", a...)
}

// Warn prints with a "[WARNING]" label.
func (p *Printer) Warn(a ...any) {
	p.printLabeled("WARNING", "yellow", a...)
}

// Error prints with an "[ERROR]" label.
func (p *Printer) Error(a ...any) {
	p.printLabeled("ERROR", "red", a...)
}

// Success prints with a "[SUCCESS]" label.
func (p *Printer) Success(a ...any) {
	p.printLabeled("SUCCESS", "green", a...)
}

func (p *Printer) printLabeled(label, colorName string, a ...any) {
	prefix := p.color(colorName) + p.color("bold") + "[" + label + "]" + p.color("end")
	parts := make([]string, 0, len(a)+1)
	parts = append(parts, prefix)
	for _, v := range a {
		parts = append(parts, p.FormatItem(v, false))
	}
	p.writeParts(parts, writeOpts{sep: " ", end: "\n"})
}

// Package-level convenience wrappers around Default, so callers who don't
// need multiple configurations can just do pp.Plain(...), pp.Warn(...), etc.
func Plain(a ...any)   { Default.Plain(a...) }
func Debug(a ...any)   { Default.Debug(a...) }
func Info(a ...any)    { Default.Info(a...) }
func Warn(a ...any)    { Default.Warn(a...) }
func Error(a ...any)   { Default.Error(a...) }
func Success(a ...any) { Default.Success(a...) }

// FormatItem formats a single value the same way Plain/Debug/etc. do,
// without printing it — useful for building your own log lines.
func FormatItem(item any) string { return Default.FormatItem(item, false) }

// ---------------------------------------------------------------------------
// Core recursive formatter (mirrors formatitem)
// ---------------------------------------------------------------------------

// FormatItem recursively renders item into a colorized string: quoted
// strings, comma-grouped numbers, {}/[] collections that wrap past WrapAt
// visible characters, struct fields shown like a map, and funcs shown as
// <function Name>.
func (p *Printer) FormatItem(item any, plainString bool) string {
	return p.formatItem(item, -2, false, plainString)
}

func (p *Printer) formatItem(item any, tab int, isArrAfterDict bool, plainString bool) (result string) {
	tab += 2

	defer func() {
		if r := recover(); r != nil {
			result = strings.Repeat(" ", tab) + p.color("red") + fmt.Sprintf("%v", item) + p.color("end")
		}
	}()

	if item == nil {
		return p.color("orange") + "nil" + p.color("end")
	}

	if b, ok := item.(bool); ok {
		if b {
			return "true"
		}
		return "false"
	}

	// error: show the message, not its (often unexported) internal struct.
	if err, ok := item.(error); ok {
		return p.formatString(err.Error())
	}

	// fmt.Stringer: prefer the custom String() over reflecting into fields.
	if s, ok := item.(fmt.Stringer); ok {
		if plainString {
			return s.String()
		}
		return p.formatString(s.String())
	}

	rv := reflect.ValueOf(item)

	// Funcs: <function Name>
	if rv.Kind() == reflect.Func {
		return p.formatFunc(rv)
	}

	// Strings
	if s, ok := item.(string); ok {
		if plainString {
			return s
		}
		return p.formatString(s)
	}

	// Numbers
	if isNumberKind(rv.Kind()) {
		return p.color("green") + formatNumber(rv) + p.color("end")
	}

	// Unwrap pointers/interfaces
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return p.color("orange") + "null" + p.color("end")
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Map:
		return p.formatMap(rv, tab, isArrAfterDict)
	case reflect.Slice, reflect.Array:
		return p.formatSlice(rv, tab, isArrAfterDict)
	case reflect.Struct:
		return p.formatStruct(rv, tab, isArrAfterDict)
	default:
		return strings.Repeat(" ", tab) + p.typeLabel(rv.Type()) +
			`"` + strings.ReplaceAll(fmt.Sprintf("%v", rv.Interface()), `"`, `\"`) + `"` +
			p.color("end")
	}
}

func (p *Printer) formatFunc(rv reflect.Value) string {
	name := "func"
	if fn := runtime.FuncForPC(rv.Pointer()); fn != nil {
		full := fn.Name()
		if idx := strings.LastIndex(full, "."); idx != -1 {
			name = full[idx+1:]
		} else {
			name = full
		}
		name = strings.TrimSuffix(name, "-fm")
	}
	return fmt.Sprintf(
		"%s<function %s%s%s%s%s>%s",
		p.color("red"), p.color("bold"), p.color("blue"), name, p.color("end"), p.color("red"), p.color("end"),
	)
}

func (p *Printer) formatString(s string) string {
	// escaped := strings.ReplaceAll(s, `\`, `\\`)
	// escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return p.color("purple") + `"` + s + `"` + p.color("end")
}

// typeLabel renders "╟TypeName╣" in pink for named types, or "" for
// anonymous/builtin ones (unnamed maps, slices, and any).
func (p *Printer) typeLabel(t reflect.Type) string {
	name := t.Name()
	if name == "" {
		return ""
	}
	switch name {
	// Skip labeling the plain builtins so map[string]any / []any stay clean.
	case "":
		return ""
	}
	return p.color("pink") + "╟" + name + "╣" + p.color("end")
}

// ---------------------------------------------------------------------------
// Numbers
// ---------------------------------------------------------------------------

func isNumberKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// formatNumber renders a number with thousands separators in the integer
// part (e.g. 1234567 -> "1,234,567", 1234.5 -> "1,234.5"), matching the
// Python implementation's regex-based grouping.
func formatNumber(rv reflect.Value) string {
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		s := strconv.FormatFloat(rv.Float(), 'f', -1, 64)
		if before, after, ok := strings.Cut(s, "."); ok {
			return groupThousands(before) + "." + after
		}
		return groupThousands(s)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return groupThousands(strconv.FormatUint(rv.Uint(), 10))
	default:
		return groupThousands(strconv.FormatInt(rv.Int(), 10))
	}
}

// groupThousands inserts "," every three digits from the right of an
// integer string, preserving a leading "-" if present.
func groupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	n := len(s)
	if n <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	lead := n % 3
	var b strings.Builder
	if lead > 0 {
		b.WriteString(s[:lead])
		if n > lead {
			b.WriteByte(',')
		}
	}
	for i := lead; i < n; i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < n {
			b.WriteByte(',')
		}
	}
	res := b.String()
	if neg {
		res = "-" + res
	}
	return res
}

// ---------------------------------------------------------------------------
// Maps / structs (rendered as "{ k: v, k: v }" or multi-line)
// ---------------------------------------------------------------------------

type kv struct {
	key   string // already formatted+colored
	value string // already formatted+colored
}

func (p *Printer) formatMap(rv reflect.Value, tab int, isArrAfterDict bool) string {
	typeName := p.typeLabel(rv.Type())

	keys := rv.MapKeys()
	if len(keys) == 0 {
		return typeName + p.color("orange") + "{}" + p.color("end")
	}

	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
	})

	entries := make([]kv, len(keys))
	for i, k := range keys {
		keyStr := k.Interface()
		var keyRendered string
		if s, ok := keyStr.(string); ok {
			keyRendered = p.color("purple") + `"` + s + `"` + p.color("end")
		} else {
			keyRendered = p.formatItem(keyStr, 0, false, false)
		}
		entries[i] = kv{
			key:   keyRendered,
			value: p.formatItem(rv.MapIndex(k).Interface(), 0, true, false),
		}
	}

	return p.renderEntries(typeName, entries, tab, isArrAfterDict)
}

func (p *Printer) formatStruct(rv reflect.Value, tab int, isArrAfterDict bool) string {
	typeName := p.typeLabel(rv.Type())
	t := rv.Type()

	var entries []kv
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" { // unexported
			continue
		}
		fv := rv.Field(i)
		entries = append(entries, kv{
			key:   p.color("purple") + `"` + field.Name + `"` + p.color("end"),
			value: p.formatItem(fv.Interface(), 0, true, false),
		})
	}

	if len(entries) == 0 {
		return typeName + p.color("orange") + "{}" + p.color("end")
	}

	return p.renderEntries(typeName, entries, tab, isArrAfterDict)
}

func (p *Printer) renderEntries(typeName string, entries []kv, tab int, isArrAfterDict bool) string {
	sep := p.color("orange") + "," + p.color("end") + " "
	single := make([]string, len(entries))
	for i, e := range entries {
		single[i] = e.key + p.color("orange") + ":" + p.color("end") + " " + e.value
	}
	indent := ""
	if !isArrAfterDict {
		indent = strings.Repeat(" ", tab)
	}
	singleLine := typeName + p.color("orange") + indent + "{ " + p.color("end") +
		strings.Join(single, sep) +
		p.color("orange") + " }" + p.color("end")

	if len(stripANSI(singleLine)) <= p.wrapAt() {
		return singleLine
	}

	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = strings.Repeat(" ", tab) + e.key + p.color("orange") + ":" + p.color("end") + " " + e.value
	}
	multiSep := p.color("orange") + "," + p.color("end") + "\n  "
	return typeName + p.color("orange") + indent + "{" + p.color("end") +
		"\n  " + strings.Join(lines, multiSep) +
		"\n" + p.color("orange") + strings.Repeat(" ", tab) + "}" + p.color("end")
}

// ---------------------------------------------------------------------------
// Slices / arrays (rendered as "[ a, b, c ]" or multi-line)
// ---------------------------------------------------------------------------

func (p *Printer) formatSlice(rv reflect.Value, tab int, isArrAfterDict bool) string {
	typeName := p.typeLabel(rv.Type())
	n := rv.Len()
	if n == 0 {
		return typeName + p.color("orange") + "[]" + p.color("end")
	}

	items := make([]string, n)
	itemIsSimple := make([]bool, n)
	for i := 0; i < n; i++ {
		v := rv.Index(i).Interface()
		items[i] = p.formatItem(v, -2, false, false)
		switch v.(type) {
		case string, int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64, float32, float64:
			itemIsSimple[i] = true
		}
	}

	indent := ""
	if !isArrAfterDict {
		indent = strings.Repeat(" ", tab)
	}

	sep := p.color("orange") + "," + p.color("end") + " "
	singleLine := typeName + p.color("orange") + indent + "[ " + p.color("end") +
		strings.Join(items, sep) +
		p.color("orange") + " ]" + p.color("end")

	if len(stripANSI(singleLine)) <= p.wrapAt() {
		return singleLine
	}

	lines := make([]string, n)
	for i, item := range items {
		prefix := ""
		if itemIsSimple[i] {
			prefix = "  " + strings.Repeat(" ", tab)
		}
		lines[i] = prefix + item
	}
	multiSep := p.color("orange") + "," + p.color("end") + "\n"
	return typeName + p.color("orange") + indent + "[\n" + p.color("end") +
		strings.Join(lines, multiSep) +
		"\n" + p.color("orange") + strings.Repeat(" ", tab) + "]" + p.color("end")
}

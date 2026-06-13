package minizinc

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Builder constructs a MiniZinc model programmatically as a typed Go API
// instead of via AddString. The Build method produces a *Model compatible
// with every existing Solve path; parameters declared with IntParam etc.
// are filled in via the normal Instance.SetParam at solve time.
//
// The builder is NOT safe for concurrent use; build the model from a single
// goroutine, then hand the resulting *Model to as many Instances as needed.
type Builder struct {
	decls       []string
	constraints []Expr
	solve       string
	includes    map[string]struct{}
	names       map[string]string // name -> kind, for collision detection
}

// NewBuilder returns an empty Builder.
func NewBuilder() *Builder {
	return &Builder{
		includes: make(map[string]struct{}),
		names:    make(map[string]string),
	}
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (b *Builder) registerName(name, kind string) {
	if !identRe.MatchString(name) {
		panic(fmt.Sprintf("minizinc: invalid identifier %q", name))
	}
	if prev, ok := b.names[name]; ok {
		panic(fmt.Sprintf("minizinc: identifier %q already declared as %s", name, prev))
	}
	b.names[name] = kind
}

// Include records `include "<file>";` to be emitted at the top of the model.
// Duplicate calls are deduplicated.
func (b *Builder) Include(file string) {
	b.includes[file] = struct{}{}
}

// IntVar declares `var lo..hi: name;` and returns the expression for it.
func (b *Builder) IntVar(name string, lo, hi int) Expr {
	b.registerName(name, "int var")
	b.decls = append(b.decls, fmt.Sprintf("var %d..%d: %s;", lo, hi, name))
	return ref(name)
}

// FloatVar declares `var lo..hi: name;` (float bounds).
func (b *Builder) FloatVar(name string, lo, hi float64) Expr {
	b.registerName(name, "float var")
	b.decls = append(b.decls, fmt.Sprintf("var %s..%s: %s;", formatFloat(lo), formatFloat(hi), name))
	return ref(name)
}

// BoolVar declares `var bool: name;`.
func (b *Builder) BoolVar(name string) Expr {
	b.registerName(name, "bool var")
	b.decls = append(b.decls, fmt.Sprintf("var bool: %s;", name))
	return ref(name)
}

// IntParam declares `int: name;`. The value is supplied at solve time via
// Instance.SetParam(name, value).
func (b *Builder) IntParam(name string) Expr {
	b.registerName(name, "int param")
	b.decls = append(b.decls, fmt.Sprintf("int: %s;", name))
	return ref(name)
}

// FloatParam declares `float: name;`.
func (b *Builder) FloatParam(name string) Expr {
	b.registerName(name, "float param")
	b.decls = append(b.decls, fmt.Sprintf("float: %s;", name))
	return ref(name)
}

// BoolParam declares `bool: name;`.
func (b *Builder) BoolParam(name string) Expr {
	b.registerName(name, "bool param")
	b.decls = append(b.decls, fmt.Sprintf("bool: %s;", name))
	return ref(name)
}

// IntArray declares `array[1..size] of var lo..hi: name;` and returns the
// array reference. Index with the At method: queens.At(Int(1)).
func (b *Builder) IntArray(name string, size, lo, hi int) Expr {
	b.registerName(name, "int array")
	b.decls = append(b.decls, fmt.Sprintf("array[1..%d] of var %d..%d: %s;", size, lo, hi, name))
	return ref(name)
}

// IntArrayParamSized declares a constant array sized by a parameter:
// `array[1..n] of int: name;`. n must be a previously declared IntParam.
func (b *Builder) IntArrayParamSized(name string, sizeParam Expr) Expr {
	b.registerName(name, "int array param")
	b.decls = append(b.decls, fmt.Sprintf("array[1..%s] of int: %s;", sizeParam.code, name))
	return ref(name)
}

// IntArrayVarSized declares an array of int variables whose size is given by
// a parameter: `array[1..n] of var lo..hi: name;`.
func (b *Builder) IntArrayVarSized(name string, sizeParam Expr, lo, hi int) Expr {
	b.registerName(name, "int array")
	b.decls = append(b.decls, fmt.Sprintf("array[1..%s] of var %d..%d: %s;", sizeParam.code, lo, hi, name))
	return ref(name)
}

// Constraint emits `constraint <expr>;`.
func (b *Builder) Constraint(e Expr) {
	b.constraints = append(b.constraints, e)
}

// Satisfy sets the solve goal to satisfaction (also the default if nothing
// is set).
func (b *Builder) Satisfy() {
	b.solve = "satisfy"
}

// Minimize sets the solve goal to `minimize <expr>`.
func (b *Builder) Minimize(e Expr) {
	b.solve = "minimize " + e.code
}

// Maximize sets the solve goal to `maximize <expr>`.
func (b *Builder) Maximize(e Expr) {
	b.solve = "maximize " + e.code
}

// Build assembles the recorded declarations, constraints and objective into
// a *Model. Subsequent builder mutations have no effect on the returned
// model.
func (b *Builder) Build() *Model {
	var sb strings.Builder

	if len(b.includes) > 0 {
		incs := make([]string, 0, len(b.includes))
		for k := range b.includes {
			incs = append(incs, k)
		}
		sort.Strings(incs)
		for _, inc := range incs {
			fmt.Fprintf(&sb, "include %q;\n", inc)
		}
	}

	for _, d := range b.decls {
		sb.WriteString(d)
		sb.WriteByte('\n')
	}

	for _, c := range b.constraints {
		sb.WriteString("constraint ")
		sb.WriteString(c.code)
		sb.WriteString(";\n")
	}

	if b.solve == "" {
		sb.WriteString("solve satisfy;\n")
	} else {
		sb.WriteString("solve ")
		sb.WriteString(b.solve)
		sb.WriteString(";\n")
	}

	m := NewModel()
	m.AddString(sb.String())
	return m
}

// Expr is an opaque MiniZinc expression value. Compose with methods like Add,
// Eq, And; pass to Constraint, Minimize, Maximize, or array indexers.
type Expr struct {
	code string
}

// String returns the rendered MiniZinc fragment for debugging.
func (e Expr) String() string { return e.code }

func ref(name string) Expr { return Expr{code: name} }

// Raw constructs an expression from a literal MiniZinc fragment. Use only
// when the typed constructors do not cover what you need.
func Raw(code string) Expr { return Expr{code: code} }

// Int returns an integer literal expression.
func Int(v int) Expr { return Expr{code: strconv.Itoa(v)} }

// Float returns a float literal expression.
func Float(v float64) Expr { return Expr{code: formatFloat(v)} }

// Bool returns a boolean literal expression.
func Bool(v bool) Expr {
	if v {
		return Expr{code: "true"}
	}
	return Expr{code: "false"}
}

// Range constructs `lo..hi`.
func Range(lo, hi Expr) Expr {
	return Expr{code: fmt.Sprintf("%s..%s", lo.code, hi.code)}
}

// At indexes an array expression: a.At(Int(1)) → `a[1]`.
func (e Expr) At(idx Expr) Expr {
	return Expr{code: fmt.Sprintf("%s[%s]", e.code, idx.code)}
}

// At2 indexes a 2-D array.
func (e Expr) At2(i, j Expr) Expr {
	return Expr{code: fmt.Sprintf("%s[%s,%s]", e.code, i.code, j.code)}
}

// Arithmetic operators
func (e Expr) Add(other Expr) Expr  { return op(e, "+", other) }
func (e Expr) Sub(other Expr) Expr  { return op(e, "-", other) }
func (e Expr) Mul(other Expr) Expr  { return op(e, "*", other) }
func (e Expr) Div(other Expr) Expr  { return op(e, "div", other) }
func (e Expr) Mod(other Expr) Expr  { return op(e, "mod", other) }
func (e Expr) FDiv(other Expr) Expr { return op(e, "/", other) }
func (e Expr) Negate() Expr         { return Expr{code: fmt.Sprintf("-(%s)", e.code)} }

// Comparisons
func (e Expr) Eq(other Expr) Expr { return op(e, "=", other) }
func (e Expr) Ne(other Expr) Expr { return op(e, "!=", other) }
func (e Expr) Lt(other Expr) Expr { return op(e, "<", other) }
func (e Expr) Le(other Expr) Expr { return op(e, "<=", other) }
func (e Expr) Gt(other Expr) Expr { return op(e, ">", other) }
func (e Expr) Ge(other Expr) Expr { return op(e, ">=", other) }

// Boolean
func (e Expr) And(other Expr) Expr     { return op(e, `/\`, other) }
func (e Expr) Or(other Expr) Expr      { return op(e, `\/`, other) }
func (e Expr) Implies(other Expr) Expr { return op(e, "->", other) }
func (e Expr) Iff(other Expr) Expr     { return op(e, "<->", other) }
func (e Expr) Not() Expr               { return Expr{code: fmt.Sprintf("not (%s)", e.code)} }

func op(a Expr, sym string, b Expr) Expr {
	return Expr{code: fmt.Sprintf("(%s %s %s)", a.code, sym, b.code)}
}

// Sum returns sum(es...).
func Sum(es ...Expr) Expr {
	return Expr{code: fmt.Sprintf("sum([%s])", strings.Join(exprCodes(es), ", "))}
}

// Forall returns `forall(idx in domain)(body)` where idx is a fresh
// identifier and domain a range.
func Forall(idx, domain, body Expr) Expr {
	return Expr{code: fmt.Sprintf("forall(%s in %s)(%s)", idx.code, domain.code, body.code)}
}

// Exists returns `exists(idx in domain)(body)`.
func Exists(idx, domain, body Expr) Expr {
	return Expr{code: fmt.Sprintf("exists(%s in %s)(%s)", idx.code, domain.code, body.code)}
}

// AllDifferent emits the global constraint. Accepts either an array variable
// reference (single argument) or a list of expressions. Adds
// `include "alldifferent.mzn";` automatically.
func (b *Builder) AllDifferent(args ...Expr) Expr {
	b.Include("alldifferent.mzn")
	if len(args) == 1 {
		return Expr{code: fmt.Sprintf("alldifferent(%s)", args[0].code)}
	}
	return Expr{code: fmt.Sprintf("alldifferent([%s])", strings.Join(exprCodes(args), ", "))}
}

// Var declares a fresh identifier for use in Forall/Exists comprehensions
// without registering it as a model-level name.
func Var(name string) Expr {
	return Expr{code: name}
}

func exprCodes(es []Expr) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.code
	}
	return out
}

// formatFloat renders a Go float64 in a form MiniZinc accepts as a float
// literal: always with a decimal point so `1.0` does not collapse to `1`.
func formatFloat(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

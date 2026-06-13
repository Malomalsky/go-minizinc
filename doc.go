// Package minizinc is a Go binding around the MiniZinc constraint solver.
//
// The package drives the `minizinc` command-line tool, captures its
// JSON-streamed output, and exposes the result through a small set of types:
// Model, Solver, Driver, Instance, Result.
//
// # Quick start
//
//	model := minizinc.NewModel()
//	model.AddString("var 1..10: x; solve maximize x;")
//
//	instance, err := minizinc.NewInstanceAuto(model)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//
//	result, err := instance.Solve(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	var out struct{ X int `json:"x"` }
//	_ = result.Decode(&out)
//
// # Building models without strings
//
// For larger models, the Builder DSL avoids string-pasting:
//
//	b := minizinc.NewBuilder()
//	x := b.IntVar("x", 1, 10)
//	b.Maximize(x)
//	model := b.Build()
//
// Identifier collisions and invalid names panic at build time. Generated
// MiniZinc code is plain text, so `Model.AddString` still composes with the
// builder output.
//
// # Streaming solutions
//
// SolveStream emits each improving solution on a channel and finally one
// terminal result carrying the proven status:
//
//	for r := range instance.SolveStream(ctx) {
//	    if r.IsIntermediate {
//	        fmt.Printf("improving: x=%v\n", r.Solution["x"])
//	        continue
//	    }
//	    fmt.Printf("final %s: x=%v\n", r.Status, r.Solution["x"])
//	}
//
// # Errors
//
// CLI failures unwrap to *MinizincError with the verbatim Stderr, ExitCode
// and a coarse Category. Use errors.Is(err, minizinc.ErrTimeout) and friends
// to branch on category.
package minizinc

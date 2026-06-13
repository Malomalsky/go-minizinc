// Example: 8-queens using the Builder DSL — no string-pasting required.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Malomalsky/go-minizinc"
)

func main() {
	b := minizinc.NewBuilder()

	n := b.IntParam("n")
	queens := b.IntArrayVarSized("queens", n, 1, 8)

	b.Constraint(b.AllDifferent(queens))

	i := minizinc.Var("i")
	j := minizinc.Var("j")
	b.Constraint(minizinc.Forall(i, minizinc.Range(minizinc.Int(1), n),
		minizinc.Forall(j, minizinc.Range(i.Add(minizinc.Int(1)), n),
			queens.At(i).Add(i).Ne(queens.At(j).Add(j)).
				And(queens.At(i).Sub(i).Ne(queens.At(j).Sub(j))),
		),
	))

	b.Satisfy()
	model := b.Build()

	instance, err := minizinc.NewInstanceAuto(model)
	if err != nil {
		log.Fatal(err)
	}
	instance.SetParam("n", 8)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := instance.Solve(ctx, minizinc.WithCommandHook(func(args []string) {
		fmt.Printf("argv: %v\n", args)
	}))
	if err != nil {
		log.Fatal(err)
	}

	var out struct {
		Queens []int `json:"queens"`
	}
	if err := result.Decode(&out); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Status: %s\n", result.Status)
	fmt.Printf("Queens: %v\n", out.Queens)
}

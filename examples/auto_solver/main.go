package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Malomalsky/go-minizinc"
)

func main() {
	model := minizinc.NewModel()
	model.AddString(`
		int: n;
		array[1..n] of var 1..n: x;
		constraint forall(i, j in 1..n where i < j)(x[i] != x[j]);
		solve satisfy;
	`)

	fmt.Println("Automatic solver selection demo")
	fmt.Println("================================")

	instance, err := minizinc.NewInstanceAuto(model)
	if err != nil {
		log.Fatal(err)
	}

	instance.SetParam("n", 5)

	fmt.Printf("Model: array of %d different integers\n", 5)
	fmt.Println("Analyzing model and selecting best solver...")

	solver, warnings, err := minizinc.FindSolverForModelWithWarnings(model)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Selected solver: %s (%s)\n", solver.Name, solver.ID)
	if len(warnings) > 0 {
		fmt.Println("Warnings:")
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	fmt.Println("\nSolving...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := instance.Solve(ctx)
	if err != nil {
		log.Fatal(err)
	}

	x, err := result.GetArray("x")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Solution: %v\n", x)
	fmt.Printf("Status: %s\n", result.Status)
}

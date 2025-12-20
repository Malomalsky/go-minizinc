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
		array[1..n] of var 1..n: queens;

		constraint forall(i,j in 1..n where i<j)(
			queens[i] != queens[j] /\
			queens[i] + i != queens[j] + j /\
			queens[i] - i != queens[j] - j
		);

		solve satisfy;
	`)

	solver, err := minizinc.FindSolver("coin-bc")
	if err != nil {
		log.Fatal(err)
	}

	instance, err := minizinc.NewInstance(model, solver)
	if err != nil {
		log.Fatal(err)
	}
	instance.SetParam("n", 8)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := instance.Solve(ctx)
	if err != nil {
		log.Fatal(err)
	}

	queens, err := result.GetArray("queens")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("8-Queens solution: %v\n", queens)
	fmt.Printf("Status: %s\n", result.Status)
}

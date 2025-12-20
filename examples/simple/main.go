package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Malomalsky/go-minizinc"
)

func main() {
	solver, err := minizinc.FindSolver("coin-bc")
	if err != nil {
		log.Fatal(err)
	}

	model := minizinc.NewModel()
	model.AddString("var 1..10: x; solve maximize x;")

	instance, err := minizinc.NewInstance(model, solver)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := instance.Solve(ctx)
	if err != nil {
		log.Fatal(err)
	}

	x, err := result.GetInt("x")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("x = %d\n", x)
	fmt.Printf("Status: %s\n", result.Status)
}

// Streaming example. NOTE: COIN-BC is a MIP solver and does not enumerate
// all solutions for satisfaction problems — for `solve satisfy` it returns
// only the first solution and prints a warning to stderr. To see every
// solution use a CP solver such as Gecode or Chuffed via FindSolver.
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
	model.AddString("var 1..10: x; var 1..10: y; constraint x + y = 10; solve satisfy;")

	instance, err := minizinc.NewInstance(model, solver)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Finding all solutions where x + y = 10:")
	count := 0
	for result := range instance.SolveStream(ctx) {
		count++
		x, _ := result.GetInt("x")
		y, _ := result.GetInt("y")
		fmt.Printf("Solution %d: x=%d, y=%d\n", count, x, y)
	}

	fmt.Printf("Total solutions found: %d\n", count)
}

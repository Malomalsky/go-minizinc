package minizinc

import (
	"context"
	"strings"
)

// Solver is the metadata of a MiniZinc solver, deserialized from
// `minizinc --solvers-json`.
type Solver struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	MznLib      string   `json:"mznlib"`
	Tags        []string `json:"tags"`
	StdFlags    []string `json:"stdFlags"`
	ExtraFlags  []any    `json:"extraFlags"`

	driver *Driver
}

// FindSolver returns the first solver whose ID, Name or any tag matches name
// case-insensitively, using the default driver.
func FindSolver(name string) (*Solver, error) {
	driver, err := DefaultDriver()
	if err != nil {
		return nil, err
	}

	return FindSolverWithDriver(name, driver)
}

// FindSolverWithDriver behaves like FindSolver but uses the given driver.
func FindSolverWithDriver(name string, driver *Driver) (*Solver, error) {
	solvers, err := driver.listSolvers(context.Background())
	if err != nil {
		return nil, err
	}

	nameLower := strings.ToLower(name)

	for i := range solvers {
		solver := &solvers[i]

		if strings.ToLower(solver.ID) == nameLower {
			return solver, nil
		}
		if strings.ToLower(solver.Name) == nameLower {
			return solver, nil
		}
		for _, tag := range solver.Tags {
			if strings.ToLower(tag) == nameLower {
				return solver, nil
			}
		}
	}

	return nil, ErrSolverNotFound
}

// ListSolvers returns all solvers known to the default driver.
func ListSolvers() ([]Solver, error) {
	driver, err := DefaultDriver()
	if err != nil {
		return nil, err
	}

	return driver.listSolvers(context.Background())
}

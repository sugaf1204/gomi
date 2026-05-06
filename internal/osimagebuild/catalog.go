package osimagebuild

import (
	"context"

	"github.com/sugaf1204/gomi/internal/oscatalog"
)

type Matrix struct {
	Include []MatrixEntry `json:"include"`
}

type MatrixEntry struct {
	Name string `json:"name"`
}

func LoadCatalog(ctx context.Context, opts LoadOptions) ([]oscatalog.Entry, error) {
	return oscatalog.Load(ctx, opts)
}

func BuildMatrix(entries []oscatalog.Entry) Matrix {
	buildEntries := oscatalog.BuildEntries(entries)
	matrix := Matrix{Include: make([]MatrixEntry, 0, len(buildEntries))}
	for _, entry := range buildEntries {
		matrix.Include = append(matrix.Include, MatrixEntry{Name: entry.Name})
	}
	return matrix
}

// Copyright 2018 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package exec

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/sql/distsqlpb"
	"github.com/cockroachdb/cockroach/pkg/sql/exec/coldata"
	"github.com/cockroachdb/cockroach/pkg/sql/exec/types"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/randutil"
)

func TestSort(t *testing.T) {
	defer leaktest.AfterTest(t)()

	tcs := []struct {
		tuples   tuples
		expected tuples
		ordCols  []distsqlpb.Ordering_Column
		typ      []types.T
	}{
		{
			tuples:   tuples{{1}, {2}, {3}, {4}, {5}, {6}, {7}},
			expected: tuples{{1}, {2}, {3}, {4}, {5}, {6}, {7}},
			typ:      []types.T{types.Int64},
			ordCols:  []distsqlpb.Ordering_Column{{ColIdx: 0}},
		},
		{
			tuples:   tuples{{1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}},
			expected: tuples{{1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}},
			typ:      []types.T{types.Int64},
			ordCols:  []distsqlpb.Ordering_Column{{ColIdx: 0}},
		},
		{
			tuples:   tuples{{1, 1}, {3, 2}, {2, 3}, {4, 4}, {5, 5}, {6, 6}, {7, 7}},
			expected: tuples{{1, 1}, {2, 3}, {3, 2}, {4, 4}, {5, 5}, {6, 6}, {7, 7}},
			typ:      []types.T{types.Int64, types.Int64},
			ordCols:  []distsqlpb.Ordering_Column{{ColIdx: 0}},
		},
		{
			tuples:   tuples{{1, 1}, {5, 2}, {3, 3}, {7, 4}, {2, 5}, {6, 6}, {4, 7}},
			expected: tuples{{1, 1}, {2, 5}, {3, 3}, {4, 7}, {5, 2}, {6, 6}, {7, 4}},
			typ:      []types.T{types.Int64, types.Int64},
			ordCols:  []distsqlpb.Ordering_Column{{ColIdx: 0}},
		},
		{
			tuples:   tuples{{1}, {5}, {3}, {3}, {2}, {6}, {4}},
			expected: tuples{{1}, {2}, {3}, {3}, {4}, {5}, {6}},
			typ:      []types.T{types.Int64},
			ordCols:  []distsqlpb.Ordering_Column{{ColIdx: 0}},
		},
		{
			tuples:   tuples{{false}, {true}},
			expected: tuples{{false}, {true}},
			typ:      []types.T{types.Bool},
			ordCols:  []distsqlpb.Ordering_Column{{ColIdx: 0}},
		},
		{
			tuples:   tuples{{true}, {false}},
			expected: tuples{{false}, {true}},
			typ:      []types.T{types.Bool},
			ordCols:  []distsqlpb.Ordering_Column{{ColIdx: 0}},
		},
		{
			tuples:   tuples{{3.2}, {2.0}, {2.4}},
			expected: tuples{{2.0}, {2.4}, {3.2}},
			typ:      []types.T{types.Float64},
			ordCols:  []distsqlpb.Ordering_Column{{ColIdx: 0}},
		},

		{
			tuples:   tuples{{0, 1, 0}, {1, 2, 0}, {2, 3, 2}, {3, 7, 1}, {4, 2, 2}},
			expected: tuples{{0, 1, 0}, {1, 2, 0}, {3, 7, 1}, {4, 2, 2}, {2, 3, 2}},
			typ:      []types.T{types.Int64, types.Int64, types.Int64},
			ordCols:  []distsqlpb.Ordering_Column{{ColIdx: 2}, {ColIdx: 1}},
		},

		{
			// ensure that sort partitions stack: make sure that a run of identical
			// values in a later column doesn't get sorted if the run is broken up
			// by previous columns.
			tuples: tuples{
				{0, 1, 0},
				{0, 1, 0},
				{0, 1, 1},
				{0, 0, 1},
				{0, 0, 0},
			},
			expected: tuples{
				{0, 0, 0},
				{0, 0, 1},
				{0, 1, 0},
				{0, 1, 0},
				{0, 1, 1},
			},
			typ:     []types.T{types.Int64, types.Int64, types.Int64},
			ordCols: []distsqlpb.Ordering_Column{{ColIdx: 0}, {ColIdx: 1}, {ColIdx: 2}},
		},
	}
	for _, tc := range tcs {
		runTests(t, []tuples{tc.tuples}, func(t *testing.T, input []Operator) {
			sort, err := NewSorter(input[0], tc.typ, tc.ordCols)
			if err != nil {
				t.Fatal(err)
			}
			cols := make([]int, len(tc.typ))
			for i := range cols {
				cols[i] = i
			}
			out := newOpTestOutput(sort, cols, tc.expected)

			if err := out.Verify(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSortRandomized(t *testing.T) {
	rng, _ := randutil.NewPseudoRand()
	nTups := 8
	maxCols := 5
	// TODO(yuzefovich): randomize types as well.
	typs := make([]types.T, maxCols)
	for i := range typs {
		typs[i] = types.Int64
	}

	for nCols := 1; nCols < maxCols; nCols++ {
		for nOrderingCols := 1; nOrderingCols <= nCols; nOrderingCols++ {
			ordCols := generateColumnOrdering(rng, nCols, nOrderingCols)
			tups := make(tuples, nTups)
			for i := range tups {
				tups[i] = make(tuple, nCols)
				for j := range tups[i] {
					// Small range so we can test partitioning
					tups[i][j] = rng.Int63() % 2048
				}
			}

			expected := make(tuples, nTups)
			copy(expected, tups)
			sort.Slice(expected, less(expected, ordCols))

			runTests(t, []tuples{tups}, func(t *testing.T, input []Operator) {
				sorter, err := NewSorter(input[0], typs[:nCols], ordCols)
				if err != nil {
					t.Fatal(err)
				}
				cols := make([]int, nCols)
				for i := range cols {
					cols[i] = i
				}
				out := newOpTestOutput(sorter, cols, expected)

				if err := out.Verify(); err != nil {
					t.Fatalf("for input %v:\n%v", tups, err)
				}
			})
		}
	}
}

func TestAllSpooler(t *testing.T) {
	defer leaktest.AfterTest(t)()

	tcs := []struct {
		tuples tuples
		typ    []types.T
	}{
		{
			tuples: tuples{{1}, {2}, {3}, {4}, {5}, {6}, {7}},
			typ:    []types.T{types.Int64},
		},
		{
			tuples: tuples{{1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}},
			typ:    []types.T{types.Int64},
		},
		{
			tuples: tuples{{1, 1}, {3, 2}, {2, 3}, {4, 4}, {5, 5}, {6, 6}, {7, 7}},
			typ:    []types.T{types.Int64, types.Int64},
		},
		{
			tuples: tuples{{1, 1}, {5, 2}, {3, 3}, {7, 4}, {2, 5}, {6, 6}, {4, 7}},
			typ:    []types.T{types.Int64, types.Int64},
		},
		{
			tuples: tuples{{1}, {5}, {3}, {3}, {2}, {6}, {4}},
			typ:    []types.T{types.Int64},
		},
		{
			tuples: tuples{{0, 1, 0}, {1, 2, 0}, {2, 3, 2}, {3, 7, 1}, {4, 2, 2}},
			typ:    []types.T{types.Int64, types.Int64, types.Int64},
		},
		{
			tuples: tuples{
				{0, 1, 0},
				{0, 1, 0},
				{0, 1, 1},
				{0, 0, 1},
				{0, 0, 0},
			},
			typ: []types.T{types.Int64, types.Int64, types.Int64},
		},
	}
	for _, tc := range tcs {
		runTests(t, []tuples{tc.tuples}, func(t *testing.T, input []Operator) {
			allSpooler := newAllSpooler(input[0], tc.typ)
			allSpooler.init()
			allSpooler.spool()
			if len(tc.tuples) != int(allSpooler.getNumTuples()) {
				t.Fatal(fmt.Sprintf("allSpooler spooled wrong number of tuples: expected %d, but received %d", len(tc.tuples), allSpooler.getNumTuples()))
			}
			if allSpooler.getPartitionsCol() != nil {
				t.Fatal("allSpooler returned non-nil partitionsCol")
			}
			for col := 0; col < len(tc.typ); col++ {
				colVec := allSpooler.getValues(col).Int64()
				for i := 0; i < int(allSpooler.getNumTuples()); i++ {
					if colVec[i] != int64(tc.tuples[i][col].(int)) {
						t.Fatal(fmt.Sprintf("allSpooler returned wrong value in %d column of %d'th tuple : expected %v, but received %v",
							col, i, tc.tuples[i][col].(int), colVec[i]))
					}
				}
			}
		})
	}
}

func BenchmarkSort(b *testing.B) {
	rng, _ := randutil.NewPseudoRand()

	for _, nBatches := range []int{1 << 1, 1 << 4, 1 << 8} {
		for _, nCols := range []int{1, 2, 4} {
			b.Run(fmt.Sprintf("rows=%d/cols=%d", nBatches*coldata.BatchSize, nCols), func(b *testing.B) {
				// 8 (bytes / int64) * nBatches (number of batches) * col.BatchSize (rows /
				// batch) * nCols (number of columns / row).
				b.SetBytes(int64(8 * nBatches * coldata.BatchSize * nCols))
				typs := make([]types.T, nCols)
				for i := range typs {
					typs[i] = types.Int64
				}
				batch := coldata.NewMemBatch(typs)
				batch.SetLength(coldata.BatchSize)
				ordCols := make([]distsqlpb.Ordering_Column, nCols)
				for i := range ordCols {
					ordCols[i].ColIdx = uint32(i)
					ordCols[i].Direction = distsqlpb.Ordering_Column_Direction(rng.Int() % 2)

					col := batch.ColVec(i).Int64()
					for j := 0; j < coldata.BatchSize; j++ {
						col[j] = rng.Int63() % int64((i*1024)+1)
					}
				}
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					source := newFiniteBatchSource(batch, nBatches)
					sort, err := NewSorter(source, typs, ordCols)
					if err != nil {
						b.Fatal(err)
					}

					sort.Init()
					for i := 0; i < nBatches; i++ {
						out := sort.Next()
						if out.Length() == 0 {
							b.Fail()
						}
					}
				}
			})
		}
	}
}

func BenchmarkAllSpooler(b *testing.B) {
	rng, _ := randutil.NewPseudoRand()

	for _, nBatches := range []int{1 << 1, 1 << 4, 1 << 8} {
		for _, nCols := range []int{1, 2, 4} {
			b.Run(fmt.Sprintf("rows=%d/cols=%d", nBatches*coldata.BatchSize, nCols), func(b *testing.B) {
				// 8 (bytes / int64) * nBatches (number of batches) * col.BatchSize (rows /
				// batch) * nCols (number of columns / row).
				b.SetBytes(int64(8 * nBatches * coldata.BatchSize * nCols))
				typs := make([]types.T, nCols)
				for i := range typs {
					typs[i] = types.Int64
				}
				batch := coldata.NewMemBatch(typs)
				batch.SetLength(coldata.BatchSize)
				for i := 0; i < nCols; i++ {
					col := batch.ColVec(i).Int64()
					for j := 0; j < coldata.BatchSize; j++ {
						col[j] = rng.Int63() % int64((i*1024)+1)
					}
				}
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					source := newFiniteBatchSource(batch, nBatches)
					allSpooler := newAllSpooler(source, typs)
					allSpooler.init()
					allSpooler.spool()
				}
			})
		}
	}
}

func less(tuples tuples, ordCols []distsqlpb.Ordering_Column) func(i, j int) bool {
	return func(i, j int) bool {
		for _, col := range ordCols {
			if tuples[i][col.ColIdx].(int64) < tuples[j][col.ColIdx].(int64) {
				return col.Direction == distsqlpb.Ordering_Column_ASC
			} else if tuples[i][col.ColIdx].(int64) > tuples[j][col.ColIdx].(int64) {
				return col.Direction == distsqlpb.Ordering_Column_DESC
			}
		}
		return false
	}
}

// generateColumnOrdering produces a random ordering of nOrderingCols columns
// on a table with nCols columns, so nOrderingCols must be not greater than
// nCols.
func generateColumnOrdering(
	rng *rand.Rand, nCols int, nOrderingCols int,
) []distsqlpb.Ordering_Column {
	if nOrderingCols > nCols {
		panic("nOrderingCols > nCols in generateColumnOrdering")
	}
	orderingCols := make([]distsqlpb.Ordering_Column, nOrderingCols)
	for i, col := range rng.Perm(nCols)[:nOrderingCols] {
		orderingCols[i] = distsqlpb.Ordering_Column{ColIdx: uint32(col), Direction: distsqlpb.Ordering_Column_Direction(rng.Intn(2))}
	}
	return orderingCols
}

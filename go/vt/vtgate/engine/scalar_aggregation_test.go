/*
Copyright 2022 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vitess.io/vitess/go/sqltypes"
)

func TestEmptyRows(outer *testing.T) {
	testCases := []struct {
		opcode      AggregateOpcode
		expectedVal string
		expectedTyp string
	}{{
		opcode:      AggregateCountDistinct,
		expectedVal: "0",
		expectedTyp: "int64",
	}, {
		opcode:      AggregateCount,
		expectedVal: "0",
		expectedTyp: "int64",
	}, {
		opcode:      AggregateSumDistinct,
		expectedVal: "null",
		expectedTyp: "decimal",
	}, {
		opcode:      AggregateSum,
		expectedVal: "null",
		expectedTyp: "int64",
	}, {
		opcode:      AggregateMax,
		expectedVal: "null",
		expectedTyp: "int64",
	}, {
		opcode:      AggregateMin,
		expectedVal: "null",
		expectedTyp: "int64",
	}}

	for _, test := range testCases {
		outer.Run(test.opcode.String(), func(t *testing.T) {
			assert := assert.New(t)
			fp := &fakePrimitive{
				results: []*sqltypes.Result{sqltypes.MakeTestResult(
					sqltypes.MakeTestFields(
						test.opcode.String(),
						"int64",
					),
					// Empty input table
				)},
			}

			oa := &ScalarAggregate{
				PreProcess: true,
				Aggregates: []*AggregateParams{{
					Opcode: test.opcode,
					Col:    0,
					Alias:  test.opcode.String(),
				}},
				Input: fp,
			}

			result, err := oa.TryExecute(&noopVCursor{}, nil, false)
			assert.NoError(err)

			wantResult := sqltypes.MakeTestResult(
				sqltypes.MakeTestFields(
					test.opcode.String(),
					test.expectedTyp,
				),
				test.expectedVal,
			)
			assert.Equal(wantResult, result)
		})
	}
}

func TestScalarAggregateStreamExecute(t *testing.T) {
	assert := assert.New(t)
	fields := sqltypes.MakeTestFields(
		"count(*)",
		"uint64",
	)
	fp := &fakePrimitive{
		allResultsInOneCall: true,
		results: []*sqltypes.Result{
			sqltypes.MakeTestResult(fields,
				"1",
			), sqltypes.MakeTestResult(fields,
				"3",
			)},
	}

	oa := &ScalarAggregate{
		Aggregates: []*AggregateParams{{
			Opcode: AggregateCount,
			Col:    0,
		}},
		Input:               fp,
		TruncateColumnCount: 1,
		PreProcess:          true,
	}

	var results []*sqltypes.Result
	err := oa.TryStreamExecute(&noopVCursor{}, nil, true, func(qr *sqltypes.Result) error {
		results = append(results, qr)
		return nil
	})
	assert.NoError(err)
	// one for the fields, and one for the actual aggregation result
	require.EqualValues(t, 2, len(results), "number of results")

	got := fmt.Sprintf("%v", results[1].Rows)
	assert.Equal("[[UINT64(4)]]", got)
}

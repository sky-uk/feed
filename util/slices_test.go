package util

import (
	"fmt"
	"reflect"
	"testing"
)

type blah interface {
}

type cat struct {
}

type input struct {
	sliceLength int
	size        int
}

type test struct {
	input  input
	result []Range
}

var tests = []test{
	{
		input: input{sliceLength: 2, size: 1},
		result: []Range{
			{Low: 0, High: 1},
			{Low: 1, High: 2},
		},
	},
	{
		input: input{sliceLength: 5, size: 2},
		result: []Range{
			{Low: 0, High: 2},
			{Low: 2, High: 4},
			{Low: 4, High: 5},
		},
	},
}

func TestSlicePartitions(t *testing.T) {
	for _, pair := range tests {
		result := Partition(pair.input.sliceLength, pair.input.size)
		fmt.Println(result)
		if !reflect.DeepEqual(pair.result, result) {
			t.Error(
				"For", pair.input,
				"expected", pair.result,
				"got", result,
			)
		}
	}
}

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSettingCSV(t *testing.T) {
	asserter := assert.New(t)

	csv := CommaSeparatedValues{}
	_ = csv.Set("first,second,third")

	asserter.Equal(CommaSeparatedValues{"first", "second", "third"}, csv)
}

func TestSettingEmptyCSV(t *testing.T) {
	asserter := assert.New(t)

	csv := CommaSeparatedValues{}
	_ = csv.Set("")

	asserter.Equal(CommaSeparatedValues{}, csv)
}

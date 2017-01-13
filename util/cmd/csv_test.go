package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSettingCSV(t *testing.T) {
	assert := assert.New(t)

	csv := CommaSeparatedValues{}
	csv.Set("first,second,third")

	assert.Equal(CommaSeparatedValues{"first", "second", "third"}, csv)
}

func TestSettingEmptyCSV(t *testing.T) {
	assert := assert.New(t)

	csv := CommaSeparatedValues{}
	csv.Set("")

	assert.Equal(CommaSeparatedValues{}, csv)
}

package cmd

import (
	"fmt"
	"strings"
)

// CommaSeparatedValues represents a slice of strings that were originally separated by ','.
type CommaSeparatedValues []string

func (c *CommaSeparatedValues) String() string {
	return fmt.Sprint(*c)
}

// Set binds a comma separated command line flag value to a KeyValue.
func (c *CommaSeparatedValues) Set(value string) error {
	if value != "" {
		*c = strings.Split(value, ",")
	}
	return nil
}

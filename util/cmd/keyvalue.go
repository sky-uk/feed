package cmd

import (
	"errors"
	"fmt"
	"strings"
)

// KeyValues for command line flag parsing of type 'key=value'
type KeyValues []KeyValue

// KeyValue is a single 'key=value' pair
type KeyValue struct {
	key   string
	value string
}

func (kv *KeyValues) String() string {
	return fmt.Sprint(*kv)
}

// Set binds a command line flag value to a KeyValue.
func (kv *KeyValues) Set(value string) error {
	keyValue := strings.Split(value, "=")
	if len(keyValue) != 2 {
		return errors.New("must be of format 'label=value'")
	}

	*kv = append(*kv, KeyValue{key: keyValue[0], value: keyValue[1]})

	return nil
}

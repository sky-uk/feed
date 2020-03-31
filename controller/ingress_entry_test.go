package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSingleInvalidAllowAddressResultsInError(t *testing.T) {
	// given
	asserter := assert.New(t)

	entry := IngressEntry{
		Host:           "my-host",
		ServiceAddress: "service",
		ServicePort:    8080,
		Allow:          []string{"invalid"},
	}

	// when
	err := entry.validate()

	// then
	asserter.Error(err)
	asserter.Equal(err.Error(), "host my-host: invalid entries in sky.uk/allow: invalid")
}

func TestSingleInvalidAllowAddressAmongValidAddressesResultsInError(t *testing.T) {
	// given
	asserter := assert.New(t)

	entry := IngressEntry{
		Host:           "my-host",
		ServiceAddress: "service",
		ServicePort:    8080,
		Allow:          []string{"1.2.3.4", "invalid", "192.168.0.1"},
	}

	// when
	err := entry.validate()

	// then
	asserter.Error(err)
	asserter.Equal(err.Error(), "host my-host: invalid entries in sky.uk/allow: invalid")
}

func TestEmptyAllowAddressResultsInError(t *testing.T) {
	// given
	asserter := assert.New(t)

	entry := IngressEntry{
		Host:           "my-host",
		ServiceAddress: "service",
		ServicePort:    8080,
		Allow:          []string{""},
	}

	// when
	err := entry.validate()

	// then
	asserter.Error(err)
	asserter.Equal(err.Error(), "host my-host: invalid entries in sky.uk/allow: <empty>")
}

func TestMultipleInvalidAllowAddressesResultInError(t *testing.T) {
	// given
	asserter := assert.New(t)

	entry := IngressEntry{
		Host:           "my-host",
		ServiceAddress: "service",
		ServicePort:    8080,
		Allow:          []string{"invalid", "invalid-2"},
	}

	// when
	err := entry.validate()

	// then
	asserter.Error(err)
	asserter.Equal(err.Error(), "host my-host: invalid entries in sky.uk/allow: invalid,invalid-2")
}

func TestValidAllowAddressResultsInNoError(t *testing.T) {
	// given
	asserter := assert.New(t)

	entry := IngressEntry{
		Host:           "my-host",
		ServiceAddress: "service",
		ServicePort:    8080,
		Allow:          []string{"127.0.0.1"},
	}

	// when
	err := entry.validate()

	// then
	asserter.NoError(err)
}

func TestValidAllowCIDRResultsInNoError(t *testing.T) {
	// given
	asserter := assert.New(t)

	entry := IngressEntry{
		Host:           "my-host",
		ServiceAddress: "service",
		ServicePort:    8080,
		Allow:          []string{"192.0.2.0/24"},
	}

	// when
	err := entry.validate()

	// then
	asserter.NoError(err)
}

func TestNoAllowAddressesResultInNoError(t *testing.T) {
	// given
	asserter := assert.New(t)

	entry := IngressEntry{
		Host:           "my-host",
		ServiceAddress: "service",
		ServicePort:    8080,
	}

	// when
	err := entry.validate()

	// then
	asserter.NoError(err)
}

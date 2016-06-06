package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidEntry(t *testing.T) {
	assert.True(t, IngressEntry{
		Host:        "valid",
		Path:        "valid",
		ServiceName: "valid",
		ServicePort: 9090,
	}.ValidateEntry())
}

func TestValidateHost(t *testing.T) {
	assert.False(t, IngressEntry{
		Host:        "",
		Path:        "valid",
		ServiceName: "valid",
		ServicePort: 9090,
	}.ValidateEntry())
	assert.False(t, IngressEntry{
		Path:        "valid",
		ServiceName: "valid",
		ServicePort: 9090,
	}.ValidateEntry())
}

func TestValidatePath(t *testing.T) {
	assert.False(t, IngressEntry{
		Host:        "valid",
		Path:        "",
		ServiceName: "valid",
		ServicePort: 9090,
	}.ValidateEntry())
	assert.False(t, IngressEntry{
		Host:        "valid",
		ServiceName: "valid",
		ServicePort: 9090,
	}.ValidateEntry())
}

func TestValidateServiceName(t *testing.T) {
	assert.False(t, IngressEntry{
		Host:        "valid",
		Path:        "valid",
		ServiceName: "",
		ServicePort: 9090,
	}.ValidateEntry())
	assert.False(t, IngressEntry{
		Host:        "valid",
		Path:        "valid",
		ServicePort: 9090,
	}.ValidateEntry())
}

func TestValidatePort(t *testing.T) {
	assert.False(t, IngressEntry{
		Host:        "valid",
		Path:        "valid",
		ServiceName: "valid",
		ServicePort: 0,
	}.ValidateEntry())
	assert.False(t, IngressEntry{
		Host:        "valid",
		Path:        "valid",
		ServiceName: "valid",
	}.ValidateEntry())
}

func TestFilterInvalidEntries(t *testing.T) {
	valid := IngressEntry{Host: "valid", Path: "valid", ServiceName: "valid", ServicePort: 9090}
	invalid := IngressEntry{}
	entries := []IngressEntry{
		valid,
		invalid,
	}

	filtered := FilterInvalidEntries(entries)

	assert.Equal(t, []IngressEntry{valid}, filtered)
}

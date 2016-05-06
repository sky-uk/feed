package feed

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeLb struct{}
type fakeClient struct{}

func TestRunIsSuccessful(t *testing.T) {
	controller := NewController(fakeLb{}, fakeClient{})

	err := controller.Run()

	assert := assert.New(t)
	assert.Nil(err, "Run should have been successful")
}

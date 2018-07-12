package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseURL(t *testing.T) {
	u, err := parseTextURL("http://localhost")
	assert.NoError(t, err)
	assert.Equal(t, u.Scheme, "http")
	assert.Equal(t, u.Host, "localhost")
}

func TestURLUnmarshal(t *testing.T) {
	u := textURL{}

	err := u.UnmarshalText([]byte("https://192.0.2.1"))
	assert.NoError(t, err)
	assert.Equal(t, u.Scheme, "https")
	assert.Equal(t, u.Host, "192.0.2.1")
}

func TestParseEmptyURL(t *testing.T) {
	u, err := parseTextURL("")
	assert.NoError(t, err)
	assert.Equal(t, u.Scheme, "")
}

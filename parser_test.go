package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigFileParsing(t *testing.T) {
	config, err := parseConfig(`
apikey = 'sk-1234567890'

[openai]
model = 'gpt-3.5-turbo'
max_tokens = 4000
temperature = 0.7
top_p = 1

	`)
	assert.NoError(t, err)

	assert.Equal(t, "sk-1234567890", config.Apikey)

	request, err := config.toRequest()
	assert.NoError(t, err)

	assert.Equal(t, "gpt-3.5-turbo", request.Model)
	assert.Equal(t, 4000, request.MaxTokens)

	var temp, topP float32 = 0.7, 1.0
	assert.Equal(t, temp, request.Temperature)
	assert.Equal(t, topP, request.TopP)
}

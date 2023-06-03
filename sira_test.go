package main

import (
	"testing"

	"github.com/sashabaranov/go-openai"
)

// TODO: This doesn't pass yet, make it so

func TestParseMessages(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		messages, err := parseTemplate(`[assistant]
Write a haiku about your favorite food.

[user]
wow that's lol`, nil)
		if err != nil {
			t.Fatal(err)
		}

		expected := []openai.ChatCompletionMessage{
			{
				Role:    "assistant",
				Content: "Write a haiku about your favorite food.",
			},
			{
				Role:    "user",
				Content: "wow that's lol",
			},
		}

		if len(messages) != len(expected) {
			t.Fatalf("expected %d messages, got %d", len(expected), len(messages))
		}

		for i, message := range messages {
			if message.Role != expected[i].Role {
				t.Fatalf("expected role %s, got %s", expected[i].Role, message.Role)
			}

			if message.Content != expected[i].Content {
				t.Fatalf("expected content %s, got %s", expected[i].Content, message.Content)
			}
		}
	})

	t.Run("conversation", func(t *testing.T) {
		system := "Write a haiku about {topic}"
		systemReplaced := "Write a haiku about rainbows"
		topic := "rainbows"

		assistant1 := `Glorious rainbow hues,
Arcsiris shines bright above,
Awe-inspiring sight.`

		user := "No, I meant a haiku about xxx"

		assistant2 := `A haiku about xxx:
xxx is a xxx
xxx is a xxx
xxx is a xxx
		- 2023, Copilot`

		template := `
[system]
` + system + `

[assistant]
` + assistant1 + `

[user]
` + user + `

[assistant]
` + assistant2

		params := map[string]any{
			"topic": topic,
		}

		messages, err := parseTemplate(template, params)
		if err != nil {
			t.Fatal(err)
		}

		if len(messages) != 4 {
			t.Fatalf("expected 4 messages, got %d", len(messages))
		}

		expected := []openai.ChatCompletionMessage{
			{
				Role:    "system",
				Content: systemReplaced,
			},
			{
				Role:    "assistant",
				Content: "Glorious rainbow hues,\nArcsiris shines bright above,\nAwe-inspiring sight.",
			},
			{
				Role:    "user",
				Content: "No, I meant a haiku about xxx",
			},
			{
				Role:    "assistant",
				Content: "A haiku about xxx:\nxxx is a xxx\nxxx is a xxx\nxxx is a xxx\n\t\t- 2023, Copilot",
			},
		}

		for i, message := range messages {
			if message.Role != expected[i].Role {
				t.Fatalf("expected role %s, got %s", expected[i].Role, message.Role)
			}

			if message.Content != expected[i].Content {
				t.Fatalf("expected content %s, got %s", expected[i].Content, message.Content)
			}
		}
	})
}

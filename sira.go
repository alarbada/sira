package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/sashabaranov/go-openai"
)

type Message = openai.ChatCompletionMessage

func execPrompt(apiKey string, req *openai.ChatCompletionRequest, messages []Message) (*Message, error) {
	client := openai.NewClient(apiKey)

	stream, err := client.CreateChatCompletionStream(context.Background(), *req)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	var newMessage Message
	newMessage.Role = "assistant"

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Println()
			break
		} else if err != nil {
			return nil, err
		}

		newMessage.Content += resp.Choices[0].Delta.Content
		fmt.Printf("%s", resp.Choices[0].Delta.Content)
	}

	return &newMessage, nil
}

func appendMessage(filename string, message Message) error {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// get last character from file
	contents, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	chars := []rune(string(contents))
	lastChar := chars[len(chars)-1]

	start := "\n\n"
	if lastChar == '\n' {
		start = "\n"
	}

	toBeAppended := fmt.Sprintf(
		"%v%v\n%s\n\n%v\n\n",
		start, TokenKind_Assistant, message.Content, TokenKind_User,
	)

	_, err = f.WriteString(toBeAppended)
	return err
}

func startTemplate(dir string) error {
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}

	paramsTemplate := `[params]
# Your params here, in the form of "param_name" = "param value"

[openai]
model = "gpt-3.5-turbo"
temperature = 0.7
max_tokens = 500
`

	err := os.WriteFile(dir+"/params.toml", []byte(paramsTemplate), 0644)
	if err != nil {
		return err
	}

	return os.WriteFile(dir+"/template.md", []byte(TokenKind_System), 0644)
}

func assertErr(err error) {
	if err != nil {
		log.Fatalf("%v", err)
	}
}

func main() {
	// disable date on log
	log.SetFlags(0)

	mainArg := os.Args[len(os.Args)-1]
	if mainArg == "help" {
		fmt.Println("Usage: sira <filename>")
		return
	}

	configContents, err := readConfig()
	if err != nil {
		log.Fatalf("could not read ~/.sira.toml file: %v", err)
	}


	config, err := parseConfig(configContents)
	if err != nil {
		log.Fatalf("could not parse ~/.sira.toml file: %v", err)
	}

	request, err := config.toRequest()
	assertErr(err)

	filename := mainArg

	messages, err := parseMessagesFromFile(filename)
	assertErr(err)

	newMessage, err := execPrompt(config.Apikey, request, messages)
	assertErr(err)

	err = appendMessage(filename, *newMessage)
	assertErr(err)
}

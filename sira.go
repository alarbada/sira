package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/sashabaranov/go-openai"
)

type Message = openai.ChatCompletionMessage

func readKey() string {
	// read ~/.sira
	f, err := os.ReadFile(os.Getenv("HOME") + "/.sira")
	if err != nil {
		panic(err)
	}

	// OPENAI_API_KEY=sk-...
	key := strings.Split(string(f), "=")[1]

	return strings.TrimSpace(key)
}

func execPrompt(params *ParamsFile, messages []Message) (*Message, error) {
	key := readKey()
	client := openai.NewClient(key)

	req := openai.ChatCompletionRequest{
		Model:       params.OpenAI.Model,
		MaxTokens:   params.OpenAI.MaxTokens,
		Temperature: params.OpenAI.Temperature,
		Messages:    messages,
		Stream:      true,
	}

	stream, err := client.CreateChatCompletionStream(context.Background(), req)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	newMessage := Message{
		Role:    "assistant",
		Content: "",
		Name:    "",
	}

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
		start = ""
	}

	_, err = f.WriteString(fmt.Sprintf("%v%v\n%s", start, TokenKind_Assistant, message.Content))
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

var (
	initFlag = flag.String("init", "", "Directory to init prompts in")
	execFlag = flag.String("exec", "", "Directory to execute prompts in")
)

func handleErr(err error) {
	// disable date on log
	log.SetFlags(0)
	log.Fatalf("Error: %v", err)
}

func main() {
	flag.Parse()

	switch {
	case *initFlag != "" && *execFlag != "":
		log.Fatal("Cannot use both -init and -exec")
	case *initFlag != "":
		if err := startTemplate(*initFlag); err != nil {
			handleErr(err)
		}
	case *execFlag != "":
		folderName := *execFlag
		params, messages, err := parseMessages(folderName)
		if err != nil {
			handleErr(err)
		}

		newMessage, err := execPrompt(params, messages)
		if err != nil {
			handleErr(err)
		}

		templateFilename := filepath.Join(folderName, "template.md")
		err = appendMessage(templateFilename, *newMessage)
		if err != nil {
			handleErr(err)
		}
	default:
		handleErr(errors.New("Must use either -init or -exec"))
	}
}

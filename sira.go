package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sashabaranov/go-openai"
)

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

type ParamsFile struct {
	Params map[string]any
	OpenAI struct {
		Model       string
		MaxTokens   int
		Temperature float32
	}
}

func stringMatches(index int, substr, content string) bool {
	if index+len(substr) >= len(content) {
		return false
	}

	return substr == content[index:index+len(substr)]
}

type TokenKind string

const (
	TokenKind_System     TokenKind = "[system]"
	TokenKind_Assistant  TokenKind = "[assistant]"
	TokenKind_User       TokenKind = "[user]"
	TokenKind_CurlyStart TokenKind = "{"
	TokenKind_CurlyEnd   TokenKind = "}"
	TokenKind_Comment    TokenKind = ">>>"
)

func (this TokenKind) ToRole() string {
	switch this {
	case TokenKind_System:
		return "system"
	case TokenKind_Assistant:
		return "assistant"
	case TokenKind_User:
		return "user"
	}

	panic("unreachable")
}

type Token struct {
	Kind TokenKind
	Pos  int
}

type TokenizerState uint8

const (
	TokenizerState_ParseRole TokenizerState = iota
	TokenizerState_ParseContent
)

type Tokenizer struct {
	State TokenizerState
}

func tokenize(rawTemplate string) []Token {
	var tokens []Token
	for i := 0; i < len(rawTemplate); i++ {

		switch {
		case stringMatches(i, string(TokenKind_System), rawTemplate):
			tokens = append(tokens, Token{
				Kind: TokenKind_System,
				Pos:  i,
			})
			i += len(TokenKind_System)
			continue

		case stringMatches(i, string(TokenKind_Assistant), rawTemplate):
			tokens = append(tokens, Token{
				Kind: TokenKind_Assistant,
				Pos:  i,
			})
			i += len(TokenKind_Assistant)
			continue

		case stringMatches(i, string(TokenKind_User), rawTemplate):
			tokens = append(tokens, Token{
				Kind: TokenKind_User,
				Pos:  i,
			})
			i += len(TokenKind_User)
			continue
		}
	}

	return tokens
}

func parseTemplate(template string, params map[string]any) ([]openai.ChatCompletionMessage, error) {
	templateFileLines := strings.Split(template, "\n")
	var withoutComments []string
	for _, line := range templateFileLines {
		if strings.Index(line, "///") != 0 {
			withoutComments = append(withoutComments, line)
		}
	}

	template = strings.Join(withoutComments, "\n")

	for k, v := range params {
		switch val := v.(type) {
		case string:
			template = strings.ReplaceAll(template, "{"+k+"}", val)
		case int64:
			template = strings.ReplaceAll(template, "{"+k+"}", fmt.Sprintf("%d", val))
		default:
			return nil, fmt.Errorf("Unknown type %T", val)
		}
	}

	var messages []openai.ChatCompletionMessage
	tokens := tokenize(template)
	for i, token := range tokens {
		isLast := i == len(tokens)-1
		if isLast {
			content := template[token.Pos+len(token.Kind):]
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    token.Kind.ToRole(),
				Content: strings.TrimSpace(content),
			})
		} else {
			content := template[token.Pos+len(token.Kind) : tokens[i+1].Pos]
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    token.Kind.ToRole(),
				Content: strings.TrimSpace(content),
			})
		}
	}

	return messages, nil
}

func parseMessages(foldername string) (*ParamsFile, []openai.ChatCompletionMessage, error) {
	dir, err := os.ReadDir(foldername)
	if err != nil {
		return nil, nil, err
	}

	var params ParamsFile
	var foundParams bool
	var templateFile string
	var foundTemplate bool
	for _, file := range dir {
		if file.Name() == "params.toml" {
			_, err := toml.DecodeFile(foldername+"/"+file.Name(), &params)
			if err != nil {
				return nil, nil, err
			}
			foundParams = true
		} else if file.Name() == "template.md" {
			f, err := os.ReadFile(foldername + "/" + file.Name())
			if err != nil {
				return nil, nil, err
			}
			templateFile = string(f)
			foundTemplate = true
		}
	}

	if !foundParams {
		return nil, nil, fmt.Errorf("params.toml not found")
	}
	if !foundTemplate {
		return nil, nil, fmt.Errorf("template.md not found")
	}

	messages, err := parseTemplate(templateFile, params.Params)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to parse template: %w", err)
	}

	return &params, messages, nil
}

func execPrompt(params *ParamsFile, messages []openai.ChatCompletionMessage) (string, error) {
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
		return "", err
	}
	defer stream.Close()

	newMessage := openai.ChatCompletionMessage{
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
			return "", err
		}

		newMessage.Content += resp.Choices[0].Delta.Content
		fmt.Printf("%s", resp.Choices[0].Delta.Content)
	}

	messages = append(messages, newMessage)

	var newContents string
	for _, message := range messages {
		newContents += `[` + message.Role + "]\n" + message.Content + "\n\n"
	}

	return newContents, nil
}

func startTemplate(dir string) error {
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}

	paramsTemplate := `[params]


[openai]
model = "gpt-3.5-turbo"
temperature = 0.7
max_tokens = 500
`

	err := os.WriteFile(dir+"/params.toml", []byte(paramsTemplate), 0644)
	if err != nil {
		return err
	}

	return os.WriteFile(dir+"/template.md", []byte(`[system]`), 0644)
}

var (
	initFlag = flag.String("init", "", "Directory to init prompts in")
	execFlag = flag.String("exec", "", "Directory to execute prompts in")
)

func main() {
	flag.Parse()

	switch {
	case *initFlag != "" && *execFlag != "":
		log.Fatal("Cannot use both -init and -exec")
	case *initFlag != "":
		if err := startTemplate(*initFlag); err != nil {
			log.Fatal(err)
		}
	case *execFlag != "":
		folderName := *execFlag
		params, messages, err := parseMessages(folderName)
		if err != nil {
			log.Fatal(err)
		}
		newTemplate, err := execPrompt(params, messages)
		if err != nil {
			log.Fatal(err)
		}

		err = os.WriteFile(folderName+"/template.md", []byte(newTemplate), 0644)
		if err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("Must use either -init or -exec")
	}
}

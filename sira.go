package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/alarbada/sira/mistral"
	"github.com/mitchellh/mapstructure"
	"github.com/sashabaranov/go-openai"
)

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

	if config.OpenAI != nil {
		request, err := config.toOpenAIRequest()
		assertErr(err)

		filename := mainArg

		messages, err := parseMessagesFromFile(filename)
		assertErr(err)

		// force getting only the latest 5 messages + the first system message,
		// otherwise it is easy to consume lots of tokens.
		// TODO: This is hardcoded, should be configurable
		if len(messages) > 5 {
			newMessages := messages[len(messages)-5:]

			firstMessage := messages[0]
			if firstMessage.Role == "system" {
				newMessages = append([]Message{firstMessage}, newMessages...)
			}

			messages = newMessages
		}

		request.Messages = messages

		newMessage, err := execOpenAIPrompt(config.Apikey, request)
		assertErr(err)

		err = appendMessage(filename, *newMessage)
		assertErr(err)
	} else if config.Mistral != nil {
		request, err := config.toMistralRequest()
		assertErr(err)

		filename := mainArg

		messages, err := parseMessagesFromFile(filename)
		assertErr(err)

		type Msg struct {
			Content *string                                    `json:"content,omitempty"`
			Role    *mistral.ChatCompletionRequestMessagesRole `json:"role,omitempty"`
		}

		for _, msg := range messages {
			role := mistral.ChatCompletionRequestMessagesRole(msg.Role)
			content := msg.Content
			request.Messages = append(request.Messages, Msg{
				Content: &content,
				Role:    &role,
			})
		}

		newMessage, err := execMistralPrompt(config.Apikey, *request)
		assertErr(err)

		err = appendMessage(filename, *newMessage)
		assertErr(err)
	}
}

func assertErr(err error) {
	if err != nil {
		log.Fatalf("%v", err)
	}
}

type Message = openai.ChatCompletionMessage

func execOpenAIPrompt(apiKey string, req *openai.ChatCompletionRequest) (*Message, error) {
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

func execMistralPrompt(apiKey string, req mistral.ChatCompletionRequest) (*Message, error) {
	client, err := mistral.NewClientWithResponses(
		"https://api.mistral.ai/v1",
		mistral.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+apiKey)
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("Could not create mistral client: %w", err)
	}

	ctx := context.Background()
	res, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()

	var newMessage Message
	newMessage.Role = "assistant"

	reader := bufio.NewReader(res.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break // End of stream
			}
			fmt.Println("Error reading stream:", err)
			break
		}
		line = strings.TrimSpace(line)
		if len(line) < len("data: ") {
			continue // Ignore lines that are too short
		} else if line == "data: [DONE]" {
			break // End of stream
		}

		line = line[len("data: "):] // Remove the "data: " prefix

		var resp struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int    `json:"created"`
			Model   string `json:"model"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Role    *string `json:"role"`
					Content string  `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			fmt.Println("Error unmarshalling JSON:", err)
			fmt.Println("tried to parse line:", line)
			break
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

func readConfig() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	bs, err := os.ReadFile(home + "/.sira.toml")
	return string(bs), err
}

type configFile struct {
	Apikey  string
	OpenAI  map[string]any
	Mistral map[string]any
}

func parseConfig(contents string) (*configFile, error) {
	params := new(configFile)
	_, err := toml.Decode(contents, params)
	return params, err
}

func (file *configFile) toOpenAIRequest() (*openai.ChatCompletionRequest, error) {
	unparsedConfig := file.OpenAI
	parsedConfig := new(openai.ChatCompletionRequest)

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:  parsedConfig,
		TagName: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("Could not create decoder: %w", err)
	}

	err = decoder.Decode(unparsedConfig)
	return parsedConfig, err
}

func (file *configFile) toMistralRequest() (*mistral.ChatCompletionRequest, error) {
	unparsedConfig := file.Mistral
	parsedConfig := new(mistral.ChatCompletionRequest)

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:  parsedConfig,
		TagName: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("Could not create decoder: %w", err)
	}

	err = decoder.Decode(unparsedConfig)
	if err != nil {
		return nil, fmt.Errorf("Could not decode config: %w", err)
	}

	if parsedConfig.TopP == nil {
		var topP float32 = 1.0
		parsedConfig.TopP = &topP
	}

	if parsedConfig.MaxTokens == nil {
		var maxTokens int = 1500
		parsedConfig.MaxTokens = &maxTokens
	}
	if parsedConfig.Temperature == nil {
		var temperature float32 = 0.7
		parsedConfig.Temperature = &temperature
	}

	stream := true
	parsedConfig.Stream = &stream

	if parsedConfig.SafeMode == nil {
		safeMode := true
		parsedConfig.SafeMode = &safeMode
	}

	if parsedConfig.RandomSeed == nil {
		var randomSeed int = 0
		parsedConfig.RandomSeed = &randomSeed
	}

	if parsedConfig.Model == "" {
		panic("model is required")
	}

	return parsedConfig, nil
}

func stringMatches(index int, substr, content string) bool {
	if index+len(substr) >= len(content) {
		return false
	}

	return substr == content[index:index+len(substr)]
}

type TokenKind string

const (
	TokenKind_System    TokenKind = "# system"
	TokenKind_Assistant TokenKind = "# assistant"
	TokenKind_User      TokenKind = "# user"
	TokenKind_Comment   TokenKind = ">>>"
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

func parseTemplate(template string, params map[string]any) ([]Message, error) {
	templateFileLines := strings.Split(template, "\n")
	var withoutComments []string
	for _, line := range templateFileLines {
		if strings.Index(line, string(TokenKind_Comment)) != 0 {
			withoutComments = append(withoutComments, line)
		}
	}

	template = strings.Join(withoutComments, "\n")

	for k, v := range params {
		if !strings.Contains(template, "{"+k+"}") {
			return nil, fmt.Errorf("Could not find parameter \"%s\" in template", k)
		}

		switch val := v.(type) {
		case string:
			template = strings.ReplaceAll(template, "{"+k+"}", val)
		case int64:
			template = strings.ReplaceAll(template, "{"+k+"}", fmt.Sprintf("%d", val))
		default:
			return nil, fmt.Errorf("Unknown type %T", val)
		}
	}

	var messages []Message
	tokens := tokenize(template)
	for i, token := range tokens {
		isLast := i == len(tokens)-1
		if isLast {
			content := template[token.Pos+len(token.Kind):]
			messages = append(messages, Message{
				Role:    token.Kind.ToRole(),
				Content: strings.TrimSpace(content),
			})
		} else {
			content := template[token.Pos+len(token.Kind) : tokens[i+1].Pos]
			messages = append(messages, Message{
				Role:    token.Kind.ToRole(),
				Content: strings.TrimSpace(content),
			})
		}
	}

	return messages, nil
}

func parseMessagesFromFile(filename string) ([]Message, error) {
	f, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	return parseTemplate(string(f), nil)
}

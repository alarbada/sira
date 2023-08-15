package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sashabaranov/go-openai"
)

type ParamsFile struct {
	Apikey string

	Params map[string]any
	OpenAI openai.ChatCompletionRequest
}

func parseParamsFile() (*ParamsFile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	var params ParamsFile
	_, err = toml.DecodeFile(home+"/.sira.toml", &params)
	return &params, err
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

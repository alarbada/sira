package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

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
	TokenKind_System     TokenKind = "# system"
	TokenKind_Assistant  TokenKind = "# assistant"
	TokenKind_User       TokenKind = "# user"
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
		if strings.Index(template, "{"+k+"}") == -1 {
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

func parseMessages(foldername string) (*ParamsFile, []Message, error) {
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


package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sashabaranov/go-openai"
)

func readKey() string {
	// read ~/.arcsiris
	f, err := os.ReadFile(os.Getenv("HOME") + "/.arcsiris")
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

func arcsiris(foldername string) error {
	dir, err := os.ReadDir(foldername)
	if err != nil {
		return err
	}

	var params ParamsFile
	var foundParams bool
	var templateFile string
	var foundTemplate bool
	for _, file := range dir {
		if file.Name() == "params.toml" {
			_, err := toml.DecodeFile(foldername+"/"+file.Name(), &params)
			if err != nil {
				return err
			}
			foundParams = true
		} else if file.Name() == "template.txt" {
			f, err := os.ReadFile(foldername + "/" + file.Name())
			if err != nil {
				return err
			}
			templateFile = string(f)
			foundTemplate = true
		}
	}

	if !foundParams {
		return fmt.Errorf("params.toml not found")
	}
	if !foundTemplate {
		return fmt.Errorf("template.txt not found")
	}

	for k, v := range params.Params {
		switch val := v.(type) {
		case string:
			templateFile = strings.ReplaceAll(templateFile, "{"+k+"}", val)
		case int64:
			templateFile = strings.ReplaceAll(templateFile, "{"+k+"}", fmt.Sprintf("%d", val))
		default:
			return fmt.Errorf("Unknown type %T", val)
		}
	}

	key := readKey()
	client := openai.NewClient(key)

	req := openai.ChatCompletionRequest{
		Model:       params.OpenAI.Model,
		MaxTokens:   params.OpenAI.MaxTokens,
		Temperature: params.OpenAI.Temperature,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: templateFile,
			},
		},
		Stream: true,
	}

	stream, err := client.CreateChatCompletionStream(context.Background(), req)
	if err != nil {
		return err
	}
	defer stream.Close()

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Println()
			return nil
		} else if err != nil {
			return err
		}

		fmt.Printf("%s", resp.Choices[0].Delta.Content)
	}
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <foldername>", os.Args[0])
	}

	foldername := os.Args[1]
	if err := arcsiris(foldername); err != nil {
		log.Fatal(err)
	}
}

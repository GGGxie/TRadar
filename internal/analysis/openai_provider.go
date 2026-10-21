package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const openAIResponsesURL = "https://api.openai.com/v1/responses"
const maxModelResponseBytes = 1 << 20

type openAIProvider struct {
	apiKey   string
	client   *http.Client
	endpoint string
}

func NewOpenAIProvider(apiKey string) (ModelProvider, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("OpenAI API key is required")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("model provider redirects are disabled")
	}}
	return newOpenAIProvider(apiKey, client, openAIResponsesURL), nil
}

func newOpenAIProvider(apiKey string, client *http.Client, endpoint string) *openAIProvider {
	return &openAIProvider{apiKey: apiKey, client: client, endpoint: endpoint}
}

func (provider *openAIProvider) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	body := map[string]any{
		"model":             request.Model,
		"instructions":      request.Instructions,
		"input":             request.Input,
		"max_output_tokens": request.MaxOutputTokens,
		"store":             false,
		"tools":             []any{},
		"tool_choice":       "none",
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "tradar_analysis", "schema": request.Schema, "strict": true,
		}},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return ModelResponse{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return ModelResponse{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+provider.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	response, err := provider.client.Do(httpRequest)
	if err != nil {
		return ModelResponse{}, err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxModelResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return ModelResponse{}, err
	}
	if len(responseBody) > maxModelResponseBytes {
		return ModelResponse{}, errors.New("model response exceeds 1 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ModelResponse{}, fmt.Errorf("model provider returned HTTP %d", response.StatusCode)
	}
	var decoded struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Content []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		} `json:"output"`
		Usage *struct {
			InputTokens  *int `json:"input_tokens"`
			OutputTokens *int `json:"output_tokens"`
		} `json:"usage"`
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	if err = decoder.Decode(&decoded); err != nil {
		return ModelResponse{}, errors.New("model provider returned invalid JSON")
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return ModelResponse{}, errors.New("model provider returned trailing JSON")
	}
	if decoded.Status != "completed" {
		return ModelResponse{}, errors.New("model provider did not complete a text response")
	}
	var output strings.Builder
	for _, item := range decoded.Output {
		if item.Type != "message" {
			continue
		}
		if item.Status != "completed" {
			return ModelResponse{}, errors.New("model provider did not complete a text response")
		}
		for _, content := range item.Content {
			switch content.Type {
			case "output_text":
				output.WriteString(content.Text)
			case "refusal":
				return ModelResponse{}, errors.New("model provider refused the response")
			default:
				return ModelResponse{}, errors.New("model provider returned unsupported message content")
			}
		}
	}
	if strings.TrimSpace(output.String()) == "" {
		return ModelResponse{}, errors.New("model provider did not complete a text response")
	}
	result := ModelResponse{Output: output.String()}
	if decoded.Usage != nil {
		result.Usage = &Usage{InputTokens: decoded.Usage.InputTokens, OutputTokens: decoded.Usage.OutputTokens}
	}
	return result, nil
}

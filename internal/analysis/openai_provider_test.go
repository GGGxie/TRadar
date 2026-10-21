package analysis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProviderSendsStrictStoredOffToolFreeRequestAndReadsUsage(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request method=%s headers=%v", r.Method, r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"reasoning"},{"type":"message","status":"completed","content":[{"type":"output_text","text":"{\"sections\":[]}"}]}],"usage":{"input_tokens":12,"output_tokens":7}}`))
	}))
	defer server.Close()

	provider := newOpenAIProvider("test-secret", server.Client(), server.URL)
	response, err := provider.Generate(context.Background(), ModelRequest{
		Model: "test-model", Instructions: "instructions", Input: "quoted evidence",
		Schema: map[string]any{"type": "object", "additionalProperties": false}, MaxOutputTokens: 321,
	})
	if err != nil || response.Output != `{"sections":[]}` || response.Usage == nil || *response.Usage.InputTokens != 12 || *response.Usage.OutputTokens != 7 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if body["model"] != "test-model" || body["store"] != false || body["tool_choice"] != "none" || body["max_output_tokens"] != float64(321) {
		t.Fatalf("body=%+v", body)
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 0 {
		t.Fatalf("tools=%#v", body["tools"])
	}
	text, ok := body["text"].(map[string]any)
	if !ok {
		t.Fatalf("text=%#v", body["text"])
	}
	format, ok := text["format"].(map[string]any)
	if !ok || format["type"] != "json_schema" || format["strict"] != true || format["name"] != "tradar_analysis" {
		t.Fatalf("format=%#v", text["format"])
	}
}

func TestOpenAIProviderRejectsRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","status":"completed","content":[{"type":"refusal","refusal":"cannot comply"}]}]}`))
	}))
	defer server.Close()
	provider := newOpenAIProvider("test-secret", server.Client(), server.URL)
	_, err := provider.Generate(context.Background(), ModelRequest{
		Model: "test-model", Instructions: "instructions", Input: "input",
		Schema: map[string]any{"type": "object", "additionalProperties": false}, MaxOutputTokens: 128,
	})
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("refusal err=%v", err)
	}
}

func TestOpenAIProviderRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxModelResponseBytes+1)))
	}))
	defer server.Close()
	provider := newOpenAIProvider("test-secret", server.Client(), server.URL)
	_, err := provider.Generate(context.Background(), ModelRequest{
		Model: "test-model", Instructions: "instructions", Input: "input",
		Schema: map[string]any{"type": "object", "additionalProperties": false}, MaxOutputTokens: 128,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("oversized response err=%v", err)
	}
}

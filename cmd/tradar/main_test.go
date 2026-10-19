package main

import "testing"

func TestValidateServerAccessRequiresLongTokenOffLoopback(t *testing.T) {
	tests := []struct {
		name, address, token string
		wantError            bool
	}{
		{"IPv4 loopback", "127.0.0.1:8080", "", false},
		{"IPv6 loopback", "[::1]:8080", "", false},
		{"localhost", "localhost:8080", "", false},
		{"public missing token", "0.0.0.0:8080", "", true},
		{"public short token", "0.0.0.0:8080", "short", true},
		{"public long token", "0.0.0.0:8080", "0123456789abcdef0123456789abcdef", false},
		{"invalid address", "localhost", "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateServerAccess(test.address, test.token)
			if (err != nil) != test.wantError {
				t.Fatalf("err=%v wantError=%t", err, test.wantError)
			}
		})
	}
}

func clearModelEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"TRADAR_MODEL_PROVIDER",
		"TRADAR_MODEL_NAME",
		"TRADAR_OPENAI_API_KEY",
		"TRADAR_MODEL_MAX_OUTPUT_TOKENS",
		"TRADAR_MODEL_DAILY_TOKEN_BUDGET",
		"TRADAR_MODEL_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}

func TestModelRuntimeIsDisabledWithoutExplicitProvider(t *testing.T) {
	clearModelEnvironment(t)
	runtime, err := modelRuntime(nil)
	if err != nil || runtime.Enabled() || runtime.Capabilities().ModelEnabled {
		t.Fatalf("runtime enabled=%t capabilities=%+v err=%v", runtime.Enabled(), runtime.Capabilities(), err)
	}
}

func TestModelRuntimeRequiresCompleteSupportedOptIn(t *testing.T) {
	clearModelEnvironment(t)
	t.Setenv("TRADAR_MODEL_PROVIDER", "openai")
	t.Setenv("TRADAR_MODEL_NAME", "configured-model")
	t.Setenv("TRADAR_OPENAI_API_KEY", "test-secret")
	t.Setenv("TRADAR_MODEL_MAX_OUTPUT_TOKENS", "800")
	t.Setenv("TRADAR_MODEL_DAILY_TOKEN_BUDGET", "100000")
	t.Setenv("TRADAR_MODEL_TIMEOUT", "30s")

	runtime, err := modelRuntime(nil)
	if err != nil || !runtime.Enabled() {
		t.Fatalf("runtime enabled=%t err=%v", runtime.Enabled(), err)
	}
	capabilities := runtime.Capabilities()
	if !capabilities.ModelEnabled || capabilities.Provider == nil || *capabilities.Provider != "openai" ||
		capabilities.Model == nil || *capabilities.Model != "configured-model" {
		t.Fatalf("capabilities=%+v", capabilities)
	}
}

func TestModelRuntimeRejectsUnsupportedOrIncompleteOptIn(t *testing.T) {
	clearModelEnvironment(t)
	t.Setenv("TRADAR_MODEL_PROVIDER", "other")
	if _, err := modelRuntime(nil); err == nil {
		t.Fatal("unsupported provider was accepted")
	}

	t.Setenv("TRADAR_MODEL_PROVIDER", "openai")
	if _, err := modelRuntime(nil); err == nil {
		t.Fatal("incomplete OpenAI configuration was accepted")
	}
}

package analysis

import (
	"context"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/trends"
)

const RulesVersion = "rules-v1"
const ModelPromptVersion = "model-analysis-v1"

type Claim struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidenceIds"`
}

type Section struct {
	Key    string  `json:"key"`
	Claims []Claim `json:"claims"`
}

type Content struct {
	Sections      []Section `json:"sections"`
	OpenQuestions []string  `json:"openQuestions"`
	NextChecks    []string  `json:"nextChecks"`
}

type CreateInput struct {
	Level             string  `json:"level"`
	ProjectRevision   int     `json:"projectRevision"`
	BasedOnAnalysisID *string `json:"basedOnAnalysisId,omitempty"`
	Content
}

type ModelInfo struct {
	Provider      string `json:"provider"`
	Name          string `json:"name"`
	PromptVersion string `json:"promptVersion"`
}

type Version struct {
	ID                string              `json:"id"`
	ProjectID         string              `json:"projectId"`
	Level             string              `json:"level"`
	Version           int                 `json:"version"`
	Origin            string              `json:"origin"`
	ProjectRevision   int                 `json:"projectRevision"`
	BasedOnAnalysisID *string             `json:"basedOnAnalysisId"`
	CreatedAt         string              `json:"createdAt"`
	CutoffAt          string              `json:"cutoffAt"`
	RulesVersion      *string             `json:"rulesVersion"`
	Model             *ModelInfo          `json:"model"`
	EvidenceIDs       []string            `json:"evidenceIds"`
	EvidenceSnapshots []catalog.Evidence  `json:"evidenceSnapshots"`
	TrendSnapshot     *trends.TrendResult `json:"trendSnapshot"`
	Content
}

type View struct {
	Version
	ContextChanged bool `json:"contextChanged"`
}

type VersionFilter struct {
	Level          string
	CutoffAt       *string
	Page, PageSize int
}

type CreateRunInput struct {
	Level           string   `json:"level"`
	Mode            string   `json:"mode"`
	ProjectRevision int      `json:"projectRevision"`
	EvidenceIDs     []string `json:"evidenceIds"`
}

type Run struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"projectId"`
	Level           string  `json:"level"`
	Mode            string  `json:"mode"`
	Status          string  `json:"status"`
	Revision        int     `json:"revision"`
	ProjectRevision int     `json:"projectRevision"`
	CutoffAt        string  `json:"cutoffAt"`
	AnalysisID      *string `json:"analysisId"`
	ErrorCode       *string `json:"errorCode"`
	Usage           *Usage  `json:"usage"`
	CreatedAt       string  `json:"createdAt"`
	FinishedAt      *string `json:"finishedAt"`
}

type Usage struct {
	InputTokens  *int `json:"inputTokens"`
	OutputTokens *int `json:"outputTokens"`
}

type ModelConfig struct {
	Provider         string
	Model            string
	PromptVersion    string
	MaxOutputTokens  int
	DailyTokenBudget int
	Timeout          time.Duration
}

func (config ModelConfig) Enabled() bool {
	return config.Provider != "" && config.Model != "" && config.MaxOutputTokens > 0 &&
		config.DailyTokenBudget > 0 && config.Timeout > 0
}

type ModelRequest struct {
	Model           string
	Instructions    string
	Input           string
	Schema          map[string]any
	MaxOutputTokens int
}

type ModelResponse struct {
	Output string
	Usage  *Usage
}

type ModelProvider interface {
	Generate(context.Context, ModelRequest) (ModelResponse, error)
}

type FrozenModelInput struct {
	Level           string             `json:"level"`
	ProjectRevision int                `json:"projectRevision"`
	CutoffAt        string             `json:"cutoffAt"`
	Evidence        []catalog.Evidence `json:"evidence"`
	Trend           trends.TrendResult `json:"trend"`
}

type Capabilities struct {
	RulesEnabled     bool    `json:"rulesEnabled"`
	ModelEnabled     bool    `json:"modelEnabled"`
	Provider         *string `json:"provider"`
	Model            *string `json:"model"`
	MaxOutputTokens  *int    `json:"maxOutputTokens"`
	DailyTokenBudget *int    `json:"dailyTokenBudget"`
}

func CurrentCapabilities(configs ...ModelConfig) Capabilities {
	result := Capabilities{RulesEnabled: true, ModelEnabled: false}
	if len(configs) == 0 || validateModelConfig(configs[0]) != nil {
		return result
	}
	config := configs[0]
	result.ModelEnabled = true
	result.Provider = &config.Provider
	result.Model = &config.Model
	result.MaxOutputTokens = &config.MaxOutputTokens
	result.DailyTokenBudget = &config.DailyTokenBudget
	return result
}

package brief

import (
	"tradar/internal/analysis"
	"tradar/internal/catalog"
	"tradar/internal/trends"
)

// brief-selection-v2（#35）：有评分 → 个人关注 → 当前关注度 → firstSeenAt；条目附项目简介与来源观测。
const SelectionVersion = "brief-selection-v2"

type Settings struct {
	Revision  int    `json:"revision"`
	IsEnabled bool   `json:"isEnabled"`
	TimeZone  string `json:"timeZone"`
	LocalTime string `json:"localTime"`
	MaxItems  int    `json:"maxItems"`
}

type SettingsPatch struct {
	IsEnabled *bool   `json:"isEnabled"`
	TimeZone  *string `json:"timeZone"`
	LocalTime *string `json:"localTime"`
	MaxItems  *int    `json:"maxItems"`
}

type Coverage struct {
	Source            string  `json:"source"`
	LatestCollectedAt *string `json:"latestCollectedAt"`
	State             string  `json:"state"`
}

type AnalysisSnapshot struct {
	ID      string           `json:"id"`
	Level   string           `json:"level"`
	Origin  string           `json:"origin"`
	Content analysis.Content `json:"content"`
}

type ResearchSnapshot struct {
	Revision   int    `json:"revision"`
	IsStarred  bool   `json:"isStarred"`
	Stage      string `json:"stage"`
	Decision   string `json:"decision"`
	NextAction string `json:"nextAction"`
}

type BriefItem struct {
	ProjectID         string             `json:"projectId"`
	ProjectRevision   int                `json:"projectRevision"`
	Title             string             `json:"title"`
	URL               string             `json:"url"`
	Summary           string             `json:"summary"`
	Reasons           []string           `json:"reasons"`
	FactSummary       string             `json:"factSummary"`
	HypothesisSummary *string            `json:"hypothesisSummary"`
	PersonalDecision  *string            `json:"personalDecision"`
	NextAction        *string            `json:"nextAction"`
	EvidenceIDs       []string           `json:"evidenceIds"`
	EvidenceSnapshots []catalog.Evidence `json:"evidenceSnapshots"`
	TrendSnapshot     trends.TrendResult `json:"trendSnapshot"`
	AnalysisID        *string            `json:"analysisId"`
	AnalysisSnapshot  *AnalysisSnapshot  `json:"analysisSnapshot"`
	ResearchRevision  *int               `json:"researchRevision"`
	ResearchSnapshot  *ResearchSnapshot  `json:"researchSnapshot"`
	ChangeFingerprint string             `json:"changeFingerprint"`
}

type DailyBrief struct {
	ID               string      `json:"id"`
	Date             string      `json:"date"`
	TimeZone         string      `json:"timeZone"`
	Version          int         `json:"version"`
	CutoffAt         string      `json:"cutoffAt"`
	GeneratedAt      string      `json:"generatedAt"`
	PreviousBriefID  *string     `json:"previousBriefId"`
	SelectionVersion string      `json:"selectionVersion"`
	RankingVersion   string      `json:"rankingVersion"`
	IsDemo           bool        `json:"isDemo"`
	Items            []BriefItem `json:"items"`
	Coverage         []Coverage  `json:"coverage"`
	EmptyReason      *string     `json:"emptyReason"`
	// OmittedItemCount 是符合变化条件但因 maxItems 未收录的数量（#59）。
	OmittedItemCount int `json:"omittedItemCount"`
}

type CreateInput struct {
	Date     string `json:"date"`
	TimeZone string `json:"timeZone"`
}

type Run struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Revision   int     `json:"revision"`
	Date       string  `json:"date"`
	TimeZone   string  `json:"timeZone"`
	CutoffAt   string  `json:"cutoffAt"`
	BriefID    *string `json:"briefId"`
	ErrorCode  *string `json:"errorCode"`
	CreatedAt  string  `json:"createdAt"`
	FinishedAt *string `json:"finishedAt"`
}

type BriefFilter struct {
	Date, TimeZone string
	Page, PageSize int
}

type CandidateSnapshot struct {
	Project      catalog.Project    `json:"project"`
	Trend        trends.TrendResult `json:"trend"`
	Evidence     []catalog.Evidence `json:"evidence"`
	Analysis     *AnalysisSnapshot  `json:"analysis"`
	Research     *ResearchSnapshot  `json:"research"`
	SeenRecently bool               `json:"seenRecently"`
	LastItem     *BriefItem         `json:"lastItem"`
}

type FrozenInput struct {
	Date             string              `json:"date"`
	TimeZone         string              `json:"timeZone"`
	CutoffAt         string              `json:"cutoffAt"`
	DiscoveryStartAt string              `json:"discoveryStartAt"`
	MaxItems         int                 `json:"maxItems"`
	HasData          bool                `json:"hasData"`
	PreviousBriefID  *string             `json:"previousBriefId"`
	Candidates       []CandidateSnapshot `json:"candidates"`
	Coverage         []Coverage          `json:"coverage"`
}

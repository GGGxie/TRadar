package brief

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"tradar/internal/analysis"
	"tradar/internal/catalog"
	"tradar/internal/trends"
)

// SelectItems applies the frozen v1 selection rules. It performs no reads so a
// queued run always produces a result from the snapshot accepted with it.
func SelectItems(input FrozenInput) ([]BriefItem, *string) {
	if !input.HasData {
		reason := "NO_DATA"
		return []BriefItem{}, &reason
	}

	type selected struct {
		candidate CandidateSnapshot
		reasons   []string
	}
	selectedItems := make([]selected, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		reasons := changeReasons(candidate, input.DiscoveryStartAt, input.CutoffAt)
		if len(reasons) != 0 {
			selectedItems = append(selectedItems, selected{candidate: candidate, reasons: reasons})
		}
	}

	// 有增长评分的项目按趋势排序；其后优先用户已收藏或研究有变化的项目，
	// 再按当前关注度和趋势模块的既定并列规则排列。
	personal := func(item selected) bool {
		return (item.candidate.Research != nil && item.candidate.Research.IsStarred) || containsReasonCode(item.reasons, "RESEARCH_CHANGE")
	}
	sort.SliceStable(selectedItems, func(i, j int) bool {
		left, right := selectedItems[i], selectedItems[j]
		leftScored, rightScored := left.candidate.Trend.Score != nil, right.candidate.Trend.Score != nil
		if leftScored != rightScored {
			return leftScored
		}
		if !leftScored && personal(left) != personal(right) {
			return personal(left)
		}
		return trends.LessProjectTrend(
			trends.ProjectTrend{Project: left.candidate.Project, Trend: left.candidate.Trend},
			trends.ProjectTrend{Project: right.candidate.Project, Trend: right.candidate.Trend},
		)
	})
	if input.MaxItems > 0 && len(selectedItems) > input.MaxItems {
		selectedItems = selectedItems[:input.MaxItems]
	}

	items := make([]BriefItem, 0, len(selectedItems))
	for _, item := range selectedItems {
		items = append(items, makeBriefItem(item.candidate, item.reasons))
	}
	if len(items) == 0 {
		reason := "NO_NEW_CHANGES"
		return items, &reason
	}
	return items, nil
}

func changeReasons(candidate CandidateSnapshot, discoveryStartAt, cutoffAt string) []string {
	reasons := make([]string, 0, 6)
	if !candidate.SeenRecently {
		if candidate.Project.FirstSeenAt > discoveryStartAt && candidate.Project.FirstSeenAt <= cutoffAt {
			reasons = append(reasons, "NEW_DISCOVERY")
		}
		return append(reasons, "FIRST_COVERAGE")
	}
	if candidate.LastItem == nil {
		return reasons
	}
	last := *candidate.LastItem
	if hasNewEvidence(candidate.Evidence, last.EvidenceSnapshots) {
		reasons = append(reasons, "NEW_EVIDENCE")
	}
	if analysisFingerprint(candidate.Analysis) != analysisFingerprint(last.AnalysisSnapshot) {
		reasons = append(reasons, "ANALYSIS_CHANGE")
	}
	if researchFingerprint(candidate.Research) != researchFingerprint(last.ResearchSnapshot) {
		reasons = append(reasons, "RESEARCH_CHANGE")
	}
	if comparableGrowthChange(last.TrendSnapshot, candidate.Trend) {
		reasons = append(reasons, "GROWTH_CHANGE")
	}
	return reasons
}

func comparableGrowthChange(previous, current trends.TrendResult) bool {
	if previous.Status != trends.StatusReady || current.Status != trends.StatusReady ||
		previous.AlgorithmVersion != current.AlgorithmVersion ||
		previous.PrimarySignal == nil || current.PrimarySignal == nil ||
		previous.PrimarySignal.SourceRecordID != current.PrimarySignal.SourceRecordID ||
		previous.PrimarySignal.Source != current.PrimarySignal.Source ||
		previous.PrimarySignal.Metric != current.PrimarySignal.Metric ||
		previous.Growth.Status != trends.StatusReady || current.Growth.Status != trends.StatusReady ||
		previous.Growth.DurationHours == nil || current.Growth.DurationHours == nil ||
		previous.Growth.RatePerDay == nil || current.Growth.RatePerDay == nil ||
		math.Abs(*previous.Growth.DurationHours-*current.Growth.DurationHours) > 0.000001 {
		return false
	}
	oldRate, newRate := *previous.Growth.RatePerDay, *current.Growth.RatePerDay
	if oldRate <= 0 {
		return newRate > 0
	}
	return newRate >= oldRate*1.5
}

func makeBriefItem(candidate CandidateSnapshot, reasons []string) BriefItem {
	evidence := append([]catalog.Evidence{}, candidate.Evidence...)
	evidenceIDs := make([]string, len(evidence))
	for index := range evidence {
		evidenceIDs[index] = evidence[index].ID
	}
	item := BriefItem{
		ProjectID: candidate.Project.ID, ProjectRevision: candidate.Project.Revision,
		Title: candidate.Project.Name, URL: candidate.Project.CanonicalURL,
		Summary: clipText(strings.TrimSpace(candidate.Project.Summary), 500),
		Reasons: append([]string{}, reasons...), FactSummary: summarizeFact(factEvidence(evidence, candidate.Project.Summary), candidate.Trend.Attention),
		HypothesisSummary: firstHypothesis(candidate.Analysis),
		EvidenceIDs:       evidenceIDs, EvidenceSnapshots: evidence,
		TrendSnapshot: candidate.Trend, AnalysisSnapshot: cloneAnalysis(candidate.Analysis),
		ResearchSnapshot:  cloneResearch(candidate.Research),
		ChangeFingerprint: candidateFingerprint(candidate),
	}
	if candidate.Analysis != nil {
		id := candidate.Analysis.ID
		item.AnalysisID = &id
	}
	if candidate.Research != nil {
		revision := candidate.Research.Revision
		decision, next := candidate.Research.Decision, strings.TrimSpace(candidate.Research.NextAction)
		item.ResearchRevision = &revision
		item.PersonalDecision = &decision
		if next != "" {
			item.NextAction = &next
		}
	}
	return item
}

var briefSourceLabels = map[string]string{"GITHUB": "GitHub", "HACKER_NEWS": "Hacker News", "PRODUCT_HUNT": "Product Hunt"}
var briefMetricLabels = map[string]string{"STARS": "Stars", "FORKS": "Forks", "POINTS": "分数", "COMMENTS": "评论", "VOTES": "票数"}

// summarizeFact 优先摘要人工或派生证据；没有证据时使用冻结趋势快照中的来源观测，
// 只陈述平台、指标、数值和观测时间，不推断需求或收入。
func summarizeFact(evidence []catalog.Evidence, attention trends.AttentionResult) string {
	if len(evidence) == 0 {
		if observed := summarizeAttention(attention); observed != "" {
			return observed
		}
		return "尚无可引用事实证据。"
	}
	item := evidence[0]
	prefix := ""
	switch item.Kind {
	case "FOUNDER_CLAIM":
		prefix = "创始人自述："
	case "THIRD_PARTY_ESTIMATE":
		prefix = "第三方估算："
	}
	return prefix + strings.TrimSpace(item.Excerpt)
}

// factEvidence 去掉正文与项目简介相同的证据（通常是派生的来源描述），
// 避免日报「简介」和「事实」两行重复；证据本身仍完整冻结在 evidenceSnapshots。
func factEvidence(evidence []catalog.Evidence, summary string) []catalog.Evidence {
	summary = normalizeText(summary)
	if summary == "" {
		return evidence
	}
	kept := make([]catalog.Evidence, 0, len(evidence))
	for _, item := range evidence {
		if normalizeText(item.Excerpt) != summary {
			kept = append(kept, item)
		}
	}
	return kept
}

func summarizeAttention(attention trends.AttentionResult) string {
	type platform struct {
		source, observedAt string
		parts              []string
	}
	platforms := make([]*platform, 0, 3)
	bySource := map[string]*platform{}
	for _, metric := range attention.Metrics {
		label, ok := briefSourceLabels[metric.Source]
		if !ok {
			continue
		}
		entry := bySource[metric.Source]
		if entry == nil {
			entry = &platform{source: label}
			bySource[metric.Source] = entry
			platforms = append(platforms, entry)
		}
		entry.parts = append(entry.parts, briefMetricLabels[metric.Metric]+" "+groupDigits(metric.Value))
		if metric.ObservedAt > entry.observedAt {
			entry.observedAt = metric.ObservedAt
		}
	}
	if len(platforms) == 0 {
		return ""
	}
	sentences := make([]string, 0, len(platforms))
	for _, entry := range platforms {
		when := entry.observedAt
		if parsed, err := time.Parse(time.RFC3339Nano, entry.observedAt); err == nil {
			when = parsed.UTC().Format("2006-01-02 15:04 UTC")
		}
		sentences = append(sentences, fmt.Sprintf("%s 累计 %s（%s）", entry.source, strings.Join(entry.parts, "、"), when))
	}
	return "来源观测：" + strings.Join(sentences, "；") + "。累计量只说明关注度。"
}

func groupDigits(value int64) string {
	raw := strconv.FormatInt(value, 10)
	sign := ""
	if strings.HasPrefix(raw, "-") {
		sign, raw = "-", raw[1:]
	}
	var builder strings.Builder
	for index, digit := range raw {
		if index > 0 && (len(raw)-index)%3 == 0 {
			builder.WriteByte(',')
		}
		builder.WriteRune(digit)
	}
	return sign + builder.String()
}

func clipText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func containsReasonCode(reasons []string, code string) bool {
	for _, reason := range reasons {
		if reason == code {
			return true
		}
	}
	return false
}

func firstHypothesis(snapshot *AnalysisSnapshot) *string {
	if snapshot == nil {
		return nil
	}
	for _, section := range snapshot.Content.Sections {
		for _, claim := range section.Claims {
			if claim.Kind == "HYPOTHESIS" && strings.TrimSpace(claim.Text) != "" {
				text := strings.TrimSpace(claim.Text)
				return &text
			}
		}
	}
	return nil
}

func candidateFingerprint(candidate CandidateSnapshot) string {
	payload := struct {
		Evidence string `json:"evidence"`
		Analysis string `json:"analysis"`
		Research string `json:"research"`
		Growth   any    `json:"growth"`
	}{
		Evidence: evidenceFingerprint(candidate.Evidence),
		Analysis: analysisFingerprint(candidate.Analysis),
		Research: researchFingerprint(candidate.Research),
		Growth:   normalizedGrowth(candidate.Trend),
	}
	body, _ := json.Marshal(payload)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

type evidenceContent struct {
	URL       string  `json:"url"`
	Title     string  `json:"title"`
	Excerpt   string  `json:"excerpt"`
	Kind      string  `json:"kind"`
	Publisher *string `json:"publisher"`
	IsPublic  bool    `json:"isPublic"`
}

func evidenceFingerprint(items []catalog.Evidence) string {
	contentSet := normalizedEvidenceSet(items)
	contents := make([]evidenceContent, 0, len(contentSet))
	for _, content := range contentSet {
		contents = append(contents, content)
	}
	sort.Slice(contents, func(i, j int) bool {
		left, _ := json.Marshal(contents[i])
		right, _ := json.Marshal(contents[j])
		return string(left) < string(right)
	})
	return hashJSON(contents)
}

func hasNewEvidence(current, previous []catalog.Evidence) bool {
	currentSet, previousSet := normalizedEvidenceSet(current), normalizedEvidenceSet(previous)
	for key := range currentSet {
		if _, existed := previousSet[key]; !existed {
			return true
		}
	}
	return false
}

func normalizedEvidenceSet(items []catalog.Evidence) map[string]evidenceContent {
	result := make(map[string]evidenceContent, len(items))
	for _, item := range items {
		if item.Kind == "METRIC_OBSERVATION" {
			continue
		}
		content := evidenceContent{
			URL: strings.TrimSpace(item.URL), Title: normalizeText(item.Title),
			Excerpt: normalizeText(item.Excerpt), Kind: item.Kind,
			Publisher: normalizeOptional(item.Publisher), IsPublic: item.IsPublic,
		}
		body, _ := json.Marshal(content)
		result[string(body)] = content
	}
	return result
}

func analysisFingerprint(snapshot *AnalysisSnapshot) string {
	if snapshot == nil {
		return ""
	}
	type normalizedClaim struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	type normalizedSection struct {
		Key    string            `json:"key"`
		Claims []normalizedClaim `json:"claims"`
	}
	payload := struct {
		Level         string              `json:"level"`
		Sections      []normalizedSection `json:"sections"`
		OpenQuestions []string            `json:"openQuestions"`
		NextChecks    []string            `json:"nextChecks"`
	}{Level: snapshot.Level, Sections: []normalizedSection{}, OpenQuestions: []string{}, NextChecks: []string{}}
	for _, section := range snapshot.Content.Sections {
		normalized := normalizedSection{Key: section.Key, Claims: []normalizedClaim{}}
		for _, claim := range section.Claims {
			normalized.Claims = append(normalized.Claims, normalizedClaim{Kind: claim.Kind, Text: normalizeText(claim.Text)})
		}
		payload.Sections = append(payload.Sections, normalized)
	}
	for _, question := range snapshot.Content.OpenQuestions {
		payload.OpenQuestions = append(payload.OpenQuestions, normalizeText(question))
	}
	for _, check := range snapshot.Content.NextChecks {
		payload.NextChecks = append(payload.NextChecks, normalizeText(check))
	}
	return hashJSON(payload)
}

func researchFingerprint(snapshot *ResearchSnapshot) string {
	if snapshot == nil {
		return ""
	}
	return hashJSON(struct {
		Stage    string `json:"stage"`
		Decision string `json:"decision"`
	}{snapshot.Stage, snapshot.Decision})
}

func normalizedGrowth(result trends.TrendResult) any {
	type growthContext struct {
		Algorithm string  `json:"algorithm"`
		Status    string  `json:"status"`
		Source    string  `json:"source,omitempty"`
		Metric    string  `json:"metric,omitempty"`
		Duration  float64 `json:"duration,omitempty"`
		Sign      string  `json:"sign,omitempty"`
	}
	context := growthContext{Algorithm: result.AlgorithmVersion, Status: string(result.Status)}
	if result.PrimarySignal != nil {
		context.Source, context.Metric = result.PrimarySignal.Source, result.PrimarySignal.Metric
	}
	if result.Growth.DurationHours != nil {
		context.Duration = *result.Growth.DurationHours
	}
	if result.Growth.RatePerDay != nil {
		switch {
		case *result.Growth.RatePerDay > 0:
			context.Sign = "POSITIVE"
		case *result.Growth.RatePerDay < 0:
			context.Sign = "NEGATIVE"
		default:
			context.Sign = "ZERO"
		}
	}
	return context
}

func hashJSON(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func normalizeText(value string) string {
	return strings.Join(strings.FieldsFunc(strings.TrimSpace(value), unicode.IsSpace), " ")
}

func normalizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := normalizeText(*value)
	return &normalized
}

func cloneAnalysis(value *AnalysisSnapshot) *AnalysisSnapshot {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Content = cloneContent(value.Content)
	return &copy
}

func cloneContent(value analysis.Content) analysis.Content {
	copy := analysis.Content{Sections: make([]analysis.Section, len(value.Sections)), OpenQuestions: append([]string{}, value.OpenQuestions...), NextChecks: append([]string{}, value.NextChecks...)}
	for index, section := range value.Sections {
		copy.Sections[index] = analysis.Section{Key: section.Key, Claims: make([]analysis.Claim, len(section.Claims))}
		for claimIndex, claim := range section.Claims {
			copy.Sections[index].Claims[claimIndex] = analysis.Claim{Kind: claim.Kind, Text: claim.Text, EvidenceIDs: append([]string{}, claim.EvidenceIDs...)}
		}
	}
	return copy
}

func cloneResearch(value *ResearchSnapshot) *ResearchSnapshot {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

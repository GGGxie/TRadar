package brief

import (
	"testing"

	"tradar/internal/analysis"
	"tradar/internal/catalog"
	"tradar/internal/trends"
)

func TestSelectItemsRequiresMeaningfulComparableChangesAndStableRanking(t *testing.T) {
	scoreHigh, scoreLow := 90.0, 20.0
	rate10, rate15, duration := 10.0, 15.0, 24.0
	first := candidate("new", "2026-10-16T01:00:00.000000000Z", &scoreLow, &rate10, &duration)
	first.SeenRecently = false
	first.Evidence = []catalog.Evidence{{ID: "new-e", ProjectID: "new", URL: "https://example.com/new", Title: "New", Excerpt: "new fact", Kind: "OFFICIAL_DESCRIPTION", CollectedAt: "2026-10-16T01:10:00.000000000Z", ObservationIDs: []string{}}}
	growth := candidate("growth", "2026-10-14T01:00:00.000000000Z", &scoreHigh, &rate15, &duration)
	growth.SeenRecently = true
	growth.LastItem = &BriefItem{ProjectID: "growth", TrendSnapshot: trend("growth", &rate10, &duration), EvidenceSnapshots: []catalog.Evidence{}, EvidenceIDs: []string{}, Reasons: []string{}}
	unchanged := candidate("unchanged", "2026-10-14T01:00:00.000000000Z", &scoreHigh, &rate10, &duration)
	unchanged.SeenRecently = true
	unchanged.LastItem = &BriefItem{ProjectID: "unchanged", TrendSnapshot: trend("unchanged", &rate10, &duration), EvidenceSnapshots: []catalog.Evidence{}, EvidenceIDs: []string{}, Reasons: []string{}}
	incomparable := candidate("incomparable", "2026-10-14T01:00:00.000000000Z", &scoreHigh, &rate15, &duration)
	incomparable.Trend.PrimarySignal.Metric = "POINTS"
	incomparable.SeenRecently = true
	incomparable.LastItem = &BriefItem{ProjectID: "incomparable", TrendSnapshot: trend("incomparable", &rate10, &duration), EvidenceSnapshots: []catalog.Evidence{}, EvidenceIDs: []string{}, Reasons: []string{}}
	input := FrozenInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai", CutoffAt: "2026-10-16T02:00:00.000000000Z", DiscoveryStartAt: "2026-10-15T02:00:00.000000000Z", MaxItems: 5, HasData: true, Candidates: []CandidateSnapshot{first, unchanged, incomparable, growth}, Coverage: []Coverage{}}
	items, empty := SelectItems(input)
	if empty != nil || len(items) != 2 || items[0].ProjectID != "growth" || items[1].ProjectID != "new" {
		t.Fatalf("items=%+v empty=%v", items, empty)
	}
	if len(items[0].Reasons) != 1 || items[0].Reasons[0] != "GROWTH_CHANGE" {
		t.Fatalf("growth reasons=%v", items[0].Reasons)
	}
	if !containsReason(items[1].Reasons, "NEW_DISCOVERY") || !containsReason(items[1].Reasons, "FIRST_COVERAGE") {
		t.Fatalf("new reasons=%v", items[1].Reasons)
	}
}

func TestSelectItemsPrefersScoredThenPersonalThenAttention(t *testing.T) {
	scored, low, high := 30.0, 0.5, 1.2
	unscored := func(id, firstSeen string, index *float64) CandidateSnapshot {
		item := candidate(id, firstSeen, nil, nil, nil)
		item.Trend.Status, item.Trend.PrimarySignal = trends.StatusInsufficientHistory, nil
		item.Trend.Attention = trends.AttentionResult{Index: index, Metrics: []trends.AttentionMetric{}}
		return item
	}
	top := candidate("scored", "2026-10-10T00:00:00.000000000Z", &scored, nil, nil)
	starred := unscored("starred", "2026-10-10T00:00:00.000000000Z", &low)
	starred.Research = &ResearchSnapshot{Revision: 1, IsStarred: true, Stage: "DEEP", Decision: "LEARN"}
	popular := unscored("popular", "2026-10-11T00:00:00.000000000Z", &high)
	newest := unscored("newest-no-attention", "2026-10-16T01:00:00.000000000Z", nil)
	input := FrozenInput{CutoffAt: "2026-10-16T02:00:00.000000000Z", DiscoveryStartAt: "2026-10-15T02:00:00.000000000Z", MaxItems: 3, HasData: true,
		Candidates: []CandidateSnapshot{newest, popular, starred, top}}
	items, empty := SelectItems(input)
	if empty != nil || len(items) != 3 {
		t.Fatalf("items=%+v empty=%v", items, empty)
	}
	got := []string{items[0].ProjectID, items[1].ProjectID, items[2].ProjectID}
	want := []string{"scored", "starred", "popular"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order=%v want=%v", got, want)
		}
	}
}

func TestBriefItemCarriesSummaryAndFrozenSourceFactsWithoutEvidence(t *testing.T) {
	item := candidate("ponytail", "2026-10-16T01:00:00.000000000Z", nil, nil, nil)
	item.Project.Summary = "Makes your AI agent lazy."
	item.Trend.Attention = trends.AttentionResult{Metrics: []trends.AttentionMetric{
		{SourceRecordID: "gh", Source: "GITHUB", Metric: "STARS", Value: 173618, ObservedAt: "2026-10-23T03:08:32.000000000Z", ObservationID: "o1"},
		{SourceRecordID: "gh", Source: "GITHUB", Metric: "FORKS", Value: 9452, ObservedAt: "2026-10-23T03:08:32.000000000Z", ObservationID: "o2"},
		{SourceRecordID: "hn", Source: "HACKER_NEWS", Metric: "POINTS", Value: 412, ObservedAt: "2026-10-23T02:21:53.000000000Z", ObservationID: "o3"},
	}}
	built := makeBriefItem(item, []string{"FIRST_COVERAGE"})
	if built.Summary != "Makes your AI agent lazy." {
		t.Fatalf("summary=%q", built.Summary)
	}
	want := "来源观测：GitHub 累计 Stars 173,618、Forks 9,452（2026-10-23 03:08 UTC）；Hacker News 累计 分数 412（2026-10-23 02:21 UTC）。累计量只说明关注度。"
	if built.FactSummary != want {
		t.Fatalf("fact=%q\nwant=%q", built.FactSummary, want)
	}
	// 派生的来源描述与简介相同：事实行不重复简介，改用来源观测。
	item.Evidence = []catalog.Evidence{{ID: "desc", ProjectID: "ponytail", Kind: "OFFICIAL_DESCRIPTION", Excerpt: " Makes your AI agent lazy. ", ObservationIDs: []string{}}}
	if repeated := makeBriefItem(item, []string{"FIRST_COVERAGE"}); repeated.FactSummary != want || len(repeated.EvidenceIDs) != 1 {
		t.Fatalf("repeated summary fact=%q evidence=%v", repeated.FactSummary, repeated.EvidenceIDs)
	}
	empty := makeBriefItem(candidate("bare", "2026-10-16T01:00:00.000000000Z", nil, nil, nil), []string{"FIRST_COVERAGE"})
	if empty.FactSummary != "尚无可引用事实证据。" || empty.Summary != "" {
		t.Fatalf("bare item=%+v", empty)
	}
}

func TestFingerprintIgnoresIdsTimesAndRepeatedEquivalentContent(t *testing.T) {
	rate, duration := 10.0, 24.0
	left := candidate("p1", "2026-10-14T00:00:00.000000000Z", nil, &rate, &duration)
	left.Evidence = []catalog.Evidence{{ID: "a", ProjectID: "p1", URL: "https://example.com/fact", Title: " Same ", Excerpt: "Evidence", Kind: "PUBLIC_EXCERPT", CollectedAt: "2026-10-15T00:00:00.000000000Z", ObservationIDs: []string{}}}
	left.Analysis = &AnalysisSnapshot{
		ID: "analysis-a", Level: "SCREEN", Origin: "MANUAL",
		Content: analysis.Content{
			Sections: []analysis.Section{{
				Key: "WHAT",
				Claims: []analysis.Claim{{
					Kind: "HYPOTHESIS", Text: " Maybe ", EvidenceIDs: []string{"a"},
				}},
			}},
			OpenQuestions: []string{}, NextChecks: []string{},
		},
	}
	right := left
	right.Evidence = []catalog.Evidence{{ID: "b", ProjectID: "p1", URL: "https://example.com/fact", Title: "Same", Excerpt: "Evidence", Kind: "PUBLIC_EXCERPT", CollectedAt: "2026-10-16T00:00:00.000000000Z", ObservationIDs: []string{}}}
	right.Analysis = &AnalysisSnapshot{ID: "analysis-b", Level: "SCREEN", Origin: "RULES", Content: left.Analysis.Content}
	if candidateFingerprint(left) != candidateFingerprint(right) {
		t.Fatalf("equivalent content changed fingerprint: %s != %s", candidateFingerprint(left), candidateFingerprint(right))
	}
}

func TestRemovedEvidenceDoesNotPretendToBeNewAndEmptyFieldsDoNotMatchFocus(t *testing.T) {
	rate, duration := 10.0, 24.0
	item := candidate("p1", "2026-10-14T00:00:00.000000000Z", nil, &rate, &duration)
	item.SeenRecently = true
	item.LastItem = &BriefItem{
		ProjectID: "p1", TrendSnapshot: item.Trend,
		EvidenceSnapshots: []catalog.Evidence{{ID: "old", ProjectID: "p1", URL: "https://example.com/fact", Title: "Fact", Excerpt: "evidence", Kind: "PUBLIC_EXCERPT"}},
		EvidenceIDs:       []string{"old"}, Reasons: []string{},
	}
	items, reason := SelectItems(FrozenInput{MaxItems: 5, HasData: true, Candidates: []CandidateSnapshot{item}})
	if len(items) != 0 || reason == nil || *reason != "NO_NEW_CHANGES" {
		t.Fatalf("removed evidence was treated as a change: items=%+v reason=%v", items, reason)
	}
	project := catalog.Project{Category: "OTHER", Name: "Unrelated", Summary: ""}
	if matchesFocus(project, []string{"AI Agent"}) {
		t.Fatal("empty summary matched every focus topic")
	}
}

func candidate(id, firstSeen string, score, rate, duration *float64) CandidateSnapshot {
	return CandidateSnapshot{
		Project: catalog.Project{ID: id, Name: id, CanonicalURL: "https://example.com/" + id, Category: "AI_AGENT", FirstSeenAt: firstSeen, Revision: 1},
		Trend:   trendWithScore(id, score, rate, duration), Evidence: []catalog.Evidence{},
	}
}

func trend(id string, rate, duration *float64) trends.TrendResult {
	return trendWithScore(id, nil, rate, duration)
}

func trendWithScore(id string, score, rate, duration *float64) trends.TrendResult {
	return trends.TrendResult{
		ProjectID: id, AsOf: "2026-10-16T02:00:00.000000000Z", AlgorithmVersion: trends.AlgorithmVersion,
		Status: trends.StatusReady, Score: score,
		PrimarySignal: &trends.PrimarySignal{SourceRecordID: "source-" + id, Source: "GITHUB", Metric: "STARS", LatestValue: 100, LatestObservedAt: "2026-10-16T02:00:00.000000000Z"},
		Growth:        trends.GrowthResult{Status: trends.StatusReady, DurationHours: duration, RatePerDay: rate, ObservationIDs: []string{}},
		CrossSource:   trends.CrossSourceResult{Platforms: []string{"GITHUB"}, SourceRecordIDs: []string{"source-" + id}}, Explanations: []string{},
	}
}

// #59：符合条件但因 maxItems 未收录的数量随版本冻结；不改变选择与排序。
func TestSelectionCountsCandidatesOmittedByMaxItems(t *testing.T) {
	input := FrozenInput{
		CutoffAt: "2026-10-16T00:00:00.000000000Z", DiscoveryStartAt: "2026-10-15T00:00:00.000000000Z",
		MaxItems: 2, HasData: true,
		Candidates: []CandidateSnapshot{candidate("a", "2026-10-15T12:00:00.000000000Z", nil, nil, nil), candidate("b", "2026-10-15T13:00:00.000000000Z", nil, nil, nil), candidate("c", "2026-10-15T14:00:00.000000000Z", nil, nil, nil)},
	}
	items, empty, omitted := SelectItemsWithOmitted(input)
	if empty != nil || len(items) != 2 || omitted != 1 {
		t.Fatalf("items=%d empty=%v omitted=%d", len(items), empty, omitted)
	}
	plain, _ := SelectItems(input)
	if plain[0].ProjectID != items[0].ProjectID || plain[1].ProjectID != items[1].ProjectID {
		t.Fatalf("selection changed: %v vs %v", plain, items)
	}
	input.MaxItems = 5
	if _, _, omitted = SelectItemsWithOmitted(input); omitted != 0 {
		t.Fatalf("nothing should be omitted under the limit, got %d", omitted)
	}
}

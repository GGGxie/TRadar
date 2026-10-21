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

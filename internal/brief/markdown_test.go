package brief

import (
	"strings"
	"testing"

	"tradar/internal/catalog"
)

func TestMarkdownUsesOnlyFrozenDataAndEscapesUntrustedStructure(t *testing.T) {
	privateNote := "PRIVATE-NOTE-MUST-NOT-LEAK"
	brief := DailyBrief{
		ID: "brief-1", Date: "2026-10-16", TimeZone: "Asia/Shanghai", Version: 2,
		CutoffAt: "2026-10-16T00:30:00.000000000Z", GeneratedAt: "2026-10-16T00:31:00.000000000Z",
		SelectionVersion: SelectionVersion, RankingVersion: "trend-v1", Coverage: []Coverage{
			{Source: "GITHUB", State: "AVAILABLE", LatestCollectedAt: pointer("2026-10-16T00:00:00.000000000Z")},
			{Source: "HACKER_NEWS", State: "NO_DATA"},
		},
		Items: []BriefItem{{
			ProjectID: "p1", Title: "Bad\n# injected <script>alert(1)</script>", URL: "javascript:alert(1)",
			Summary: "帮助开发者 *追踪*\n# 变化",
			Reasons: []string{"FIRST_COVERAGE"}, FactSummary: "创始人自述：有 10 位付费用户\n> forged quote",
			PersonalDecision: pointer("OBSERVE"), NextAction: pointer("核对 [留存](javascript:alert(2))"),
			EvidenceIDs: []string{"e1"}, EvidenceSnapshots: []catalog.Evidence{{
				ID: "e1", ProjectID: "p1", URL: "https://example.com/fact?q=1)", Title: "证据\n## fake",
				Excerpt: "创始人称已有付费用户", Kind: "FOUNDER_CLAIM", CollectedAt: "2026-10-16T00:00:00.000000000Z",
			}},
		}},
	}
	first := RenderMarkdown(brief)
	second := RenderMarkdown(brief)
	if first != second {
		t.Fatal("same frozen brief produced different Markdown")
	}
	for _, forbidden := range []string{"<script>", "javascript:", privateNote, "\n# injected", "\n## fake", "\n> forged quote"} {
		if strings.Contains(first, forbidden) {
			t.Fatalf("unsafe or private content %q leaked:\n%s", forbidden, first)
		}
	}
	for _, required := range []string{"TRadar 日报", "2026-10-16", "创始人自述", "FIRST_COVERAGE", "https://example.com/fact?q=1%29", "- **简介**：帮助开发者 \\*追踪\\* \\# 变化", "- **个人判断**：继续观察"} {
		if !strings.Contains(first, required) {
			t.Fatalf("missing %q:\n%s", required, first)
		}
	}
}

func pointer(value string) *string { return &value }

// #58：导出按日报时区显示到分钟，保留 UTC 原值便于核对。
func TestMarkdownShowsTimesInBriefTimeZone(t *testing.T) {
	output := RenderMarkdown(DailyBrief{
		Date: "2026-10-16", TimeZone: "Asia/Shanghai", Version: 1,
		CutoffAt: "2026-10-16T00:30:00.123456789Z", GeneratedAt: "2026-10-16T00:31:05.000000000Z",
		SelectionVersion: SelectionVersion, RankingVersion: "trend-v1",
		Coverage: []Coverage{{Source: "GITHUB", State: "AVAILABLE", LatestCollectedAt: pointer("2026-10-15T23:59:59.000000000Z")}},
	})
	for _, required := range []string{
		"- **数据截止**：2026-10-16 08:30（Asia/Shanghai；UTC 2026-10-16T00:30:00Z）",
		"- **生成时间**：2026-10-16 08:31（Asia/Shanghai）",
		"最近采集 2026-10-16 07:59（Asia/Shanghai）",
	} {
		if !strings.Contains(output, required) {
			t.Fatalf("missing %q:\n%s", required, output)
		}
	}
	if strings.Contains(output, ".123456789") {
		t.Fatalf("raw nanosecond timestamp leaked:\n%s", output)
	}
}

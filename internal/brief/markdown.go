package brief

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

// 与界面 researchDecisionLabels 一致；未知值原样输出。
var decisionLabels = map[string]string{
	"UNDECIDED": "尚未决定", "OBSERVE": "继续观察", "LEARN": "值得学习",
	"USE": "准备使用", "VALIDATE": "需要验证", "PASS": "暂时放弃",
}

var reasonLabels = map[string]string{
	"NEW_DISCOVERY":   "新发现",
	"FIRST_COVERAGE":  "首次覆盖",
	"GROWTH_CHANGE":   "增长变化",
	"NEW_EVIDENCE":    "新增证据",
	"ANALYSIS_CHANGE": "分析变化",
	"RESEARCH_CHANGE": "个人判断变化",
}

var unsafeTextScheme = regexp.MustCompile(`(?i)\b(javascript|vbscript|data):`)

var coverageLabels = map[string]string{
	"AVAILABLE": "已有新鲜数据", "STALE": "数据超过 24 小时", "NO_DATA": "尚无数据",
}

// RenderMarkdown renders only the immutable stored snapshot. All external text
// is flattened and escaped so it cannot introduce headings, HTML, or links.
func RenderMarkdown(brief DailyBrief) string {
	var output strings.Builder
	output.WriteString("# TRadar 日报\n\n")
	writeField(&output, "日期", brief.Date)
	writeField(&output, "时区", brief.TimeZone)
	writeField(&output, "版本", fmt.Sprintf("v%d", brief.Version))
	writeField(&output, "数据截止", brief.CutoffAt)
	writeField(&output, "生成时间", brief.GeneratedAt)
	writeField(&output, "选择规则", brief.SelectionVersion)
	writeField(&output, "趋势算法", brief.RankingVersion)

	output.WriteString("\n## 数据覆盖\n\n")
	for _, coverage := range brief.Coverage {
		state := coverageLabels[coverage.State]
		if state == "" {
			state = coverage.State
		}
		line := fmt.Sprintf("- %s：%s", escapeMarkdownText(coverage.Source), escapeMarkdownText(state))
		if coverage.LatestCollectedAt != nil {
			line += "；最近采集 " + escapeMarkdownText(*coverage.LatestCollectedAt)
		}
		output.WriteString(line + "\n")
	}

	output.WriteString("\n## 今日变化\n\n")
	if len(brief.Items) == 0 {
		reason := "今天没有符合条件的新变化。"
		if brief.EmptyReason != nil && *brief.EmptyReason == "NO_DATA" {
			reason = "尚无可用于生成日报的数据。"
		}
		output.WriteString(reason + "\n")
		return output.String()
	}
	for index, item := range brief.Items {
		output.WriteString(fmt.Sprintf("### %d. %s\n\n", index+1, escapeMarkdownText(item.Title)))
		reasons := make([]string, 0, len(item.Reasons))
		for _, reason := range item.Reasons {
			label := reasonLabels[reason]
			if label != "" {
				reasons = append(reasons, label+" (`"+reason+"`)")
			} else {
				reasons = append(reasons, escapeMarkdownText(reason))
			}
		}
		output.WriteString("- **变化原因**：" + strings.Join(reasons, "、") + "\n")
		if strings.TrimSpace(item.Summary) != "" {
			writeField(&output, "简介", item.Summary)
		}
		writeField(&output, "事实", item.FactSummary)
		if item.HypothesisSummary != nil {
			writeField(&output, "已存推测", *item.HypothesisSummary)
		}
		if item.PersonalDecision != nil {
			decision := *item.PersonalDecision
			if label, ok := decisionLabels[decision]; ok {
				decision = label
			}
			writeField(&output, "个人判断", decision)
		}
		if item.NextAction != nil {
			writeField(&output, "下一步", *item.NextAction)
		}
		writeURLField(&output, "项目链接", item.URL)
		writeTrend(&output, item)
		if len(item.EvidenceSnapshots) != 0 {
			output.WriteString("- 引用证据：\n")
			for evidenceIndex, evidence := range item.EvidenceSnapshots {
				output.WriteString(fmt.Sprintf("  %d. %s；类型 %s；采集于 %s；摘录：%s",
					evidenceIndex+1, escapeMarkdownText(evidence.Title), escapeMarkdownText(evidence.Kind),
					escapeMarkdownText(evidence.CollectedAt), escapeMarkdownText(evidence.Excerpt)))
				if safe := safeMarkdownURL(evidence.URL); safe != "" {
					output.WriteString("；来源 <" + safe + ">")
				}
				output.WriteString("\n")
			}
		}
		output.WriteString("\n")
	}
	return output.String()
}

func writeTrend(output *strings.Builder, item BriefItem) {
	trend := item.TrendSnapshot
	if trend.PrimarySignal == nil {
		writeField(output, "趋势", "样本不足，未评分")
		return
	}
	text := fmt.Sprintf("%s %s，当前累计值 %d", trend.PrimarySignal.Source, trend.PrimarySignal.Metric, trend.PrimarySignal.LatestValue)
	if trend.Growth.RatePerDay != nil && trend.Growth.DurationHours != nil {
		text += fmt.Sprintf("；%.0f 小时窗口日均变化 %.2f", *trend.Growth.DurationHours, *trend.Growth.RatePerDay)
	}
	writeField(output, "趋势", text)
}

func writeField(output *strings.Builder, label, value string) {
	output.WriteString("- **" + escapeMarkdownText(label) + "**：" + escapeMarkdownText(value) + "\n")
}

func writeURLField(output *strings.Builder, label, raw string) {
	if safe := safeMarkdownURL(raw); safe != "" {
		output.WriteString("- **" + escapeMarkdownText(label) + "**：<" + safe + ">\n")
	}
}

func escapeMarkdownText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = html.EscapeString(value)
	value = unsafeTextScheme.ReplaceAllString(value, "$1：")
	return strings.NewReplacer(
		"\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_",
		"{", "\\{", "}", "\\}", "[", "\\[", "]", "\\]",
		"(", "\\(", ")", "\\)", "#", "\\#", "+", "\\+",
		"!", "\\!", "|", "\\|", ">", "\\>",
	).Replace(value)
}

func safeMarkdownURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return strings.NewReplacer("<", "%3C", ">", "%3E", "(", "%28", ")", "%29", " ", "%20").Replace(parsed.String())
}

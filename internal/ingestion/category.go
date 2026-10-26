package ingestion

import (
	"strings"
	"unicode"
)

// 方向归类只在采集首次创建项目时使用；用户之后修改的分类不会被覆盖。
// 规则按 AI Agent → 开发者工具 → SaaS 的顺序匹配整词（英文）或词组（中文），
// 都不命中时保持「其他方向」，宁可少归类也不猜测。
var categoryRules = []struct {
	category string
	words    []string
	phrases  []string
}{
	{"AI_AGENT",
		[]string{"agent", "agents", "agentic", "mcp", "llm", "llms", "copilot", "chatbot", "rag", "claude", "chatgpt", "openai", "anthropic"},
		[]string{"智能体", "大模型", "智能代理"}},
	{"DEVTOOLS",
		[]string{"cli", "sdk", "api", "apis", "ide", "editor", "compiler", "debugger", "framework", "library",
			"devtools", "devtool", "developer", "developers", "terminal", "git", "linter", "css", "npm", "kubernetes", "docker"},
		[]string{"开发者", "开发工具", "调试", "编辑器", "框架"}},
	{"SAAS",
		[]string{"saas", "b2b", "subscription", "crm", "erp", "tenant", "invoicing", "billing"},
		[]string{"软件即服务", "订阅"}},
}

func classifyCategory(texts ...string) string {
	joined := strings.ToLower(strings.Join(texts, " "))
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(joined, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		words[word] = true
	}
	for _, rule := range categoryRules {
		for _, word := range rule.words {
			if words[word] {
				return rule.category
			}
		}
		for _, phrase := range rule.phrases {
			if strings.Contains(joined, phrase) {
				return rule.category
			}
		}
	}
	return "OTHER"
}

// hackerNewsProjectName 去掉发布类前缀，项目名只保留作品本身；HN 来源记录仍保存原标题。
// Ask/Tell HN 是讨论而非作品，保持原样。
func hackerNewsProjectName(title string) string {
	for _, prefix := range []string{"show hn:", "launch hn:"} {
		if len(title) > len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
			if name := strings.TrimSpace(title[len(prefix):]); name != "" {
				return name
			}
		}
	}
	return title
}

package ingestion

import "testing"

func TestHackerNewsProjectNameDropsLaunchPrefixes(t *testing.T) {
	cases := map[string]string{
		"Show HN: Forked – Git for AI agent state": "Forked – Git for AI agent state",
		"show hn:  Sumi.css – CSS Framework":       "Sumi.css – CSS Framework",
		"Launch HN: Omnara (YC S25)":               "Omnara (YC S25)",
		"Tell HN: I broke the PHP RCE embargo":     "Tell HN: I broke the PHP RCE embargo",
		"Show HN without link":                     "Show HN without link",
		"Show HN:":                                 "Show HN:",
	}
	for title, want := range cases {
		if got := hackerNewsProjectName(title); got != want {
			t.Errorf("hackerNewsProjectName(%q)=%q want %q", title, got, want)
		}
	}
}

func TestClassifyCategoryUsesWholeWordsWithAgentFirst(t *testing.T) {
	cases := []struct {
		name, text, want string
	}{
		// 真实采集样例（2026-10-23 验收库）
		{"agent prompt", "DietrichGebert/ponytail Makes your AI agent think like the laziest senior dev in the room.", "AI_AGENT"},
		{"agents plural", "OpenShell – A safe, private runtime for fleets of autonomous AI agents", "AI_AGENT"},
		{"mcp", "Open-source auth gateway connecting 1500+ SaaS providers to AI agents through SDK, CLI, MCP", "AI_AGENT"},
		{"agentic", "Agentic workflows for your repo", "AI_AGENT"},
		{"coding skill", "A skill to stop your coding agent from burying the answer.", "AI_AGENT"},
		{"claude skills", "Eleven free Claude skills that clone any app: reverse-engineer it, rebuild it", "AI_AGENT"},
		{"chatgpt", "Use ChatGPT from your menu bar", "AI_AGENT"},
		{"devtools", "Hexal – Modal text editor with feature parity with Helix, Lua plugins", "DEVTOOLS"},
		{"cli", "A fast CLI for Postgres migrations", "DEVTOOLS"},
		{"framework", "Sumi.css – CSS Framework", "DEVTOOLS"},
		{"saas", "funtastic-saas", "SAAS"},
		{"multi tenant", "Stock module of Simpul: multi-warehouse, multi-tenant inventory", "SAAS"},
		{"other", "Quad9 (DNS Resolver) to Exit France", "OTHER"},
		{"other news", "Woman faces six years in prison over Cash App account she never opened", "OTHER"},
		// 词边界：不能因为 "agency"、"management"、"capital" 误判
		{"no partial agent", "A creative agency portfolio", "OTHER"},
		{"no partial api", "Capital markets explained", "OTHER"},
		{"chinese agent", "开源智能体平台", "AI_AGENT"},
		{"chinese devtools", "面向开发者的调试工具", "DEVTOOLS"},
	}
	for _, item := range cases {
		if got := classifyCategory(item.text); got != item.want {
			t.Errorf("%s: classifyCategory(%q)=%s want %s", item.name, item.text, got, item.want)
		}
	}
}

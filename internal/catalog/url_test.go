package catalog

import "testing"

func TestNormalizeURLPreservesIdentity(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://EXAMPLE.com:443/app?plan=pro&utm_source=x#pricing", "https://example.com/app?plan=pro"},
		{"https://example.com/a/", "https://example.com/a/"},
		{"http://example.com/a", "http://example.com/a"},
		{"https://github.com/Owner/Repo.git/?utm_medium=share", "https://github.com/owner/repo"},
		{"https://example.com", "https://example.com/"},
		// #37：代码浏览路径归一到仓库根，避免同一仓库因 HN 链接到某个 commit 而成为另一个项目。
		{"https://github.com/Zof-AI/zof-api-contract-check/tree/e9d45f612e75406b1f943c7bab76fab600fa950f", "https://github.com/zof-ai/zof-api-contract-check"},
		{"https://github.com/acme/tool/blob/main/README.md#usage", "https://github.com/acme/tool"},
		{"https://github.com/acme/tool/tree/main/", "https://github.com/acme/tool"},
		// 其他子页面（issues、releases、wiki…）保持原样，不擅自合并成仓库。
		{"https://github.com/acme/tool/issues/12", "https://github.com/acme/tool/issues/12"},
		{"https://github.com/acme/tool/tree", "https://github.com/acme/tool/tree"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := NormalizeURL(tc.in)
			if err != nil || got != tc.want {
				t.Fatalf("got %q,%v want %q", got, err, tc.want)
			}
		})
	}
}
func TestNormalizeURLRejectsUnsafeInputs(t *testing.T) {
	for _, u := range []string{"javascript:alert(1)", "data:text/plain,x", "https://u:p@example.com/", "/relative", "https://", "https://example.com:wat", "https://example.com/\n"} {
		t.Run(u, func(t *testing.T) {
			if _, err := NormalizeURL(u); err == nil {
				t.Fatal("accepted invalid URL")
			}
		})
	}
}

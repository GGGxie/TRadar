package catalog

import "testing"

func TestNormalizeURLPreservesIdentity(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://EXAMPLE.com:443/app?plan=pro&utm_source=x#pricing", "https://example.com/app?plan=pro"},
		{"https://example.com/a/", "https://example.com/a/"},
		{"http://example.com/a", "http://example.com/a"},
		{"https://github.com/Owner/Repo.git/?utm_medium=share", "https://github.com/owner/repo"},
		{"https://example.com", "https://example.com/"},
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

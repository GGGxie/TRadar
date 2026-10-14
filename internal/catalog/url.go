package catalog

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"
)

// NormalizeURL removes only known tracking information. It never fetches a URL.
func NormalizeURL(raw string) (string, error) {
	if len(raw) > 4096 || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("invalid URL")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("only absolute HTTP(S) URLs without credentials are allowed")
	}
	host := strings.ToLower(u.Hostname())
	if strings.ContainsAny(host, " \\/") {
		return "", fmt.Errorf("invalid host")
	}
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	u.Fragment = ""
	u.RawFragment = ""
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", fmt.Errorf("invalid query")
	}
	for key := range query {
		if strings.HasPrefix(strings.ToLower(key), "utm_") || key == "gclid" || key == "fbclid" {
			query.Del(key)
		}
	}
	u.RawQuery = query.Encode()
	u.ForceQuery = false
	if u.Path == "" {
		u.Path = "/"
	}
	if host == "github.com" && port == "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			u.Scheme = "https"
			u.Path = "/" + strings.ToLower(parts[0]) + "/" + strings.ToLower(strings.TrimSuffix(parts[1], ".git"))
			u.RawPath = ""
		}
	}
	return u.String(), nil
}

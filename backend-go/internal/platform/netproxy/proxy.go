// Package netproxy applies the user's proxy settings to outbound HTTP clients.
package netproxy

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
)

var errInvalidProxy = errors.New("invalid Windows proxy setting")

// Transport preserves Go's default HTTP behavior while adding the Windows
// manual system proxy when the process has no proxy environment variable.
func Transport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = ForRequest
	return transport
}

// ForRequest gives explicit process settings priority over the Windows system
// proxy. This matters for desktop launches, which often do not inherit shell
// variables even when the browser uses the Windows proxy.
func ForRequest(req *http.Request) (*url.URL, error) {
	if hasEnvironmentProxy(req.URL.Scheme) {
		return http.ProxyFromEnvironment(req)
	}
	return systemProxy(req)
}

func hasEnvironmentProxy(scheme string) bool {
	keys := []string{"HTTPS_PROXY", "https_proxy"}
	if scheme == "http" {
		keys = []string{"HTTP_PROXY", "http_proxy"}
	}
	for _, key := range keys {
		if _, exists := os.LookupEnv(key); exists {
			return true
		}
	}
	return false
}

func fromManualSettings(req *http.Request, server, bypass string) (*url.URL, error) {
	host := strings.ToLower(req.URL.Hostname())
	if isBypassed(host, bypass) || isBypassed(host, os.Getenv("NO_PROXY")) || isBypassed(host, os.Getenv("no_proxy")) {
		return nil, nil
	}
	endpoint := strings.TrimSpace(server)
	if strings.Contains(endpoint, "=") {
		endpoint = ""
		for _, part := range strings.Split(server, ";") {
			name, value, found := strings.Cut(strings.TrimSpace(part), "=")
			if found && strings.EqualFold(strings.TrimSpace(name), req.URL.Scheme) {
				endpoint = strings.TrimSpace(value)
				break
			}
		}
	}
	if endpoint == "" {
		return nil, nil
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	proxy, err := url.Parse(endpoint)
	if err != nil || proxy.Host == "" || proxy.User != nil || (proxy.Scheme != "http" && proxy.Scheme != "https" && proxy.Scheme != "socks5") {
		return nil, errInvalidProxy
	}
	return proxy, nil
}

func isBypassed(host, patterns string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	for _, raw := range strings.FieldsFunc(patterns, func(r rune) bool { return r == ';' || r == ',' }) {
		pattern := strings.ToLower(strings.TrimSpace(raw))
		if pattern == "<local>" && !strings.Contains(host, ".") {
			return true
		}
		if pattern == host {
			return true
		}
		if matched, err := path.Match(pattern, host); err == nil && matched {
			return true
		}
	}
	return false
}

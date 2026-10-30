//go:build windows

package netproxy

import (
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/sys/windows/registry"
)

const windowsInternetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

func systemProxy(req *http.Request) (*url.URL, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsInternetSettings, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil
	}
	defer key.Close()
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil, nil
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return nil, fmt.Errorf("read Windows proxy server: %w", err)
	}
	bypass, _, _ := key.GetStringValue("ProxyOverride")
	return fromManualSettings(req, server, bypass)
}

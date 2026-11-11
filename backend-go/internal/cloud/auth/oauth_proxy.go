package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

func oauthLoopbackProxyTransport(value string) (*http.Transport, error) {
	proxy, err := url.Parse(value)
	if err != nil || proxy.Scheme != "http" || proxy.Hostname() != "127.0.0.1" ||
		proxy.User != nil || proxy.Path != "" || proxy.RawQuery != "" || proxy.ForceQuery || proxy.Fragment != "" {
		return nil, errors.New("OAuth proxy must be an HTTP loopback endpoint")
	}
	port, err := strconv.ParseUint(proxy.Port(), 10, 16)
	if err != nil || port == 0 {
		return nil, errors.New("OAuth proxy requires a valid port")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxy)
	return transport, nil
}

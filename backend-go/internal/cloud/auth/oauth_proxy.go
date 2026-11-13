package auth

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"
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
	transport := oauthDirectTransport()
	transport.Proxy = http.ProxyURL(proxy)
	return transport, nil
}

func oauthDirectTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return transport
}

type oauthRoutes []http.RoundTripper

func newOAuthRoutes(proxyURLs []string, directFirst bool) (oauthRoutes, error) {
	if len(proxyURLs) > 2 {
		return nil, errors.New("at most two OAuth relays may be configured")
	}
	var routes oauthRoutes
	if directFirst {
		routes = append(routes, oauthDirectTransport())
	}
	seen := make(map[string]bool)
	for _, endpoint := range proxyURLs {
		if endpoint == "" || seen[endpoint] {
			continue
		}
		transport, err := oauthLoopbackProxyTransport(endpoint)
		if err != nil {
			return nil, err
		}
		seen[endpoint] = true
		routes = append(routes, transport)
	}
	if !directFirst {
		routes = append(routes, oauthDirectTransport())
	}
	return routes, nil
}

func (routes oauthRoutes) RoundTrip(request *http.Request) (*http.Response, error) {
	var last error
	for index, route := range routes {
		if err := request.Context().Err(); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(request.Context(), 6*time.Second)
		var originReached atomic.Bool
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(httptrace.GotConnInfo) { originReached.Store(true) },
		})
		attempt := request.Clone(ctx)
		if index > 0 && request.Body != nil {
			if request.GetBody == nil {
				cancel()
				return nil, last
			}
			body, err := request.GetBody()
			if err != nil {
				cancel()
				return nil, err
			}
			attempt.Body = body
		}
		response, err := route.RoundTrip(attempt)
		if err == nil {
			// A response proves the code may have been consumed. Only safe
			// reads can switch routes after a provider or gateway response.
			if request.Method == http.MethodGet && index+1 < len(routes) &&
				(response.StatusCode == 502 || response.StatusCode == 503 || response.StatusCode == 504) {
				response.Body.Close()
				cancel()
				continue
			}
			response.Body = &oauthResponseBody{ReadCloser: response.Body, cancel: cancel}
			return response, nil
		}
		cancel()
		last = err
		// GotConn fires after CONNECT and the origin TLS handshake, before
		// any request headers or code. After that point be conservative.
		if request.Method != http.MethodGet && originReached.Load() {
			return nil, err
		}
	}
	return nil, last
}

type oauthResponseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *oauthResponseBody) Close() error {
	defer b.cancel()
	return b.ReadCloser.Close()
}

package inference

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"
)

var nonPublicRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var publicIPv6 = netip.MustParsePrefix("2000::/3")

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !publicIPv6.Contains(ip)) {
		return false
	}
	for _, prefix := range nonPublicRanges {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// PublicTransport resolves each new connection and dials only the checked IP.
func PublicTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		lastErr := errors.New("upstream hostname has no public IP address")
		for _, ip := range ips {
			if !publicAddress(ip) {
				continue
			}
			attempt, stop := context.WithTimeout(ctx, 4*time.Second)
			conn, err := dialer.DialContext(attempt, network, net.JoinHostPort(ip.Unmap().String(), port))
			stop()
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return transport
}

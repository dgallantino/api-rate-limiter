package httplimit

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

type KeyFunc func(*http.Request) (string, error)

func KeyFromHeader(name string) KeyFunc {
	return func(r *http.Request) (string, error) {
		v := strings.TrimSpace(r.Header.Get(name))
		if v == "" {
			return "", fmt.Errorf("httplimit: missing header %s", name)
		}
		return v, nil
	}
}

func KeyFromIP() KeyFunc {
	return func(r *http.Request) (string, error) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		host = strings.TrimSpace(host)
		if host == "" {
			return "", fmt.Errorf("httplimit: missing remote addr")
		}
		return host, nil
	}
}

func ParseKeySource(spec string) (KeyFunc, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return KeyFromHeader("X-API-Key"), nil
	}
	if strings.EqualFold(s, "ip") {
		return KeyFromIP(), nil
	}
	const p = "header:"
	if len(s) >= len(p) && strings.EqualFold(s[:len(p)], p) {
		name := strings.TrimSpace(s[len(p):])
		if name == "" {
			return nil, fmt.Errorf("httplimit: empty header name")
		}
		return KeyFromHeader(name), nil
	}
	return nil, fmt.Errorf("httplimit: key source %q: want header:<name> or ip", spec)
}

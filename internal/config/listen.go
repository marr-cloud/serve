// Package config parses CLI flags and listen URIs into a typed configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ParseListenURI normalizes a user-supplied listen string into a canonical
// host:port suitable for net.Listen("tcp", …).
//
// Accepted forms:
//   - "3000"               → "0.0.0.0:3000"
//   - ":3000"              → "0.0.0.0:3000"
//   - "host:3000"          → "host:3000"
//   - "tcp://host:3000"    → "host:3000"
//   - "127.0.0.1:3000"     → "127.0.0.1:3000"
//   - "[::1]:3000"         → "[::1]:3000"
//
// "unix://" and "pipe://" return a "not supported" error (planned for F3).
func ParseListenURI(input string) (string, error) {
	if input == "" {
		return "", errors.New("empty listen address")
	}
	// Check for scheme (either "scheme://" or specific schemes like "unix:" and "pipe:")
	if idx := strings.Index(input, "://"); idx >= 0 {
		scheme := strings.ToLower(input[:idx])
		rest := input[idx+3:]
		switch scheme {
		case "tcp":
			input = rest
		case "unix", "pipe":
			return "", fmt.Errorf("scheme %q not supported in this version", scheme)
		default:
			return "", fmt.Errorf("scheme %q not supported", scheme)
		}
	} else {
		// Check for unix: and pipe: schemes (without //)
		if strings.HasPrefix(input, "unix:") {
			return "", fmt.Errorf("scheme %q not supported in this version", "unix")
		}
		if strings.HasPrefix(input, "pipe:") {
			return "", fmt.Errorf("scheme %q not supported in this version", "pipe")
		}
	}
	if strings.HasPrefix(input, "[") {
		host, port, err := net.SplitHostPort(input)
		if err != nil {
			return "", fmt.Errorf("invalid IPv6 address %q: %w", input, err)
		}
		if err := validatePort(port); err != nil {
			return "", err
		}
		return net.JoinHostPort(host, port), nil
	}
	if !strings.Contains(input, ":") {
		if err := validatePort(input); err != nil {
			return "", err
		}
		return "0.0.0.0:" + input, nil
	}
	if strings.HasPrefix(input, ":") {
		port := input[1:]
		if err := validatePort(port); err != nil {
			return "", err
		}
		return "0.0.0.0:" + port, nil
	}
	host, port, err := net.SplitHostPort(input)
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", input, err)
	}
	if err := validatePort(port); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, port), nil
}

// ParseListenURIScheme parses a listen URI and returns its scheme
// ("tcp" | "unix" | "pipe") and the transport-specific address.
// For unix, addr is a filesystem path. For pipe, it's the raw pipe path
// (e.g. `\\.\pipe\serve`). For tcp, it's `host:port` (canonicalized by
// ParseListenURI). Unknown schemes return an error.
func ParseListenURIScheme(input string) (scheme, addr string, err error) {
	switch {
	case strings.HasPrefix(input, "unix://"):
		return "unix", strings.TrimPrefix(input, "unix://"), nil
	case strings.HasPrefix(input, "unix:"):
		return "unix", strings.TrimPrefix(input, "unix:"), nil
	case strings.HasPrefix(input, "pipe://"):
		return "pipe", strings.TrimPrefix(input, "pipe://"), nil
	case strings.HasPrefix(input, "pipe:"):
		return "pipe", strings.TrimPrefix(input, "pipe:"), nil
	}
	addr, err = ParseListenURI(input)
	if err != nil {
		return "", "", err
	}
	return "tcp", addr, nil
}

func validatePort(p string) error {
	n, err := strconv.Atoi(p)
	if err != nil {
		return fmt.Errorf("invalid port %q", p)
	}
	if n < 0 || n > 65535 {
		return fmt.Errorf("invalid port %d (must be 0-65535)", n)
	}
	return nil
}

package delivery

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

func validateRemoteURL(value string) error {
	if value == "" || strings.ContainsAny(value, "\x00\r\n\t ") || strings.HasPrefix(value, "-") {
		return errors.New("delivery remote URL is unsafe")
	}
	if filepath.IsAbs(value) {
		if filepath.Clean(value) != value {
			return errors.New("delivery local remote path is not canonical")
		}
		return nil
	}
	if !strings.Contains(value, "://") {
		authority, remotePath, ok := strings.Cut(value, ":")
		user, host, hasUser := strings.Cut(authority, "@")
		if !ok || !hasUser || !sshUserPattern.MatchString(user) || !remoteHostPattern.MatchString(host) || remotePath == "" ||
			strings.ContainsAny(host+remotePath, "@?#") || strings.Contains(remotePath, "..") {
			return errors.New("delivery remote URL uses an unsupported SSH form")
		}
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Path == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("delivery remote URL is invalid")
	}
	if parsed.Scheme == "file" {
		if parsed.User != nil || parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") || !filepath.IsAbs(parsed.Path) {
			return errors.New("delivery file remote URL is unsafe")
		}
		return nil
	}
	if parsed.Host == "" || !remoteHostPattern.MatchString(parsed.Hostname()) {
		return errors.New("delivery remote URL is invalid")
	}
	if strings.EqualFold(parsed.Hostname(), "github.com") && parsed.Port() != "" {
		return errors.New("delivery GitHub remote URL must not contain an explicit port")
	}
	switch parsed.Scheme {
	case "https":
		if parsed.User != nil {
			return errors.New("delivery HTTPS remote URL must not contain credentials")
		}
	case "ssh":
		if parsed.User != nil {
			if _, hasPassword := parsed.User.Password(); hasPassword || !sshUserPattern.MatchString(parsed.User.Username()) {
				return errors.New("delivery SSH remote URL contains unsupported credentials")
			}
		}
	default:
		return errors.New("delivery remote URL uses an unsupported protocol")
	}
	return nil
}

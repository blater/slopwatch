package delivery

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	remoteAliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	sshUserPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	remoteHostPattern  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
)

func remoteRepositoryURL(value string) string {
	if strings.HasPrefix(value, "git@github.com:") {
		return normalizeGitHubRepository(strings.TrimPrefix(value, "git@github.com:"))
	}
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return ""
	}
	return normalizeGitHubRepository(strings.TrimPrefix(parsed.Path, "/"))
}

func normalizeGitHubRepository(value string) string {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".git")
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}

func validRemoteAlias(value string) bool { return remoteAliasPattern.MatchString(value) }

// canonicalLocalRemote resolves mutable symlink components before admission.
func canonicalLocalRemote(value string) (string, error) {
	if filepath.IsAbs(value) {
		canonical, err := filepath.EvalSymlinks(value)
		if err != nil {
			return "", errors.New("delivery local remote cannot be canonicalized")
		}
		return canonical, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "file" {
		return value, nil
	}
	canonical, err := filepath.EvalSymlinks(parsed.Path)
	if err != nil {
		return "", errors.New("delivery local remote cannot be canonicalized")
	}
	return (&url.URL{Scheme: "file", Path: canonical}).String(), nil
}

func remoteProviderTarget(value string) (PreflightResult, error) {
	if filepath.IsAbs(value) || strings.HasPrefix(value, "file://") {
		return PreflightResult{}, nil
	}
	var host, remotePath string
	if !strings.Contains(value, "://") {
		authority, path, ok := strings.Cut(value, ":")
		if !ok {
			return PreflightResult{}, errors.New("remote provider identity is unavailable")
		}
		_, host, _ = strings.Cut(authority, "@")
		remotePath = path
	} else {
		parsed, err := url.Parse(value)
		if err != nil {
			return PreflightResult{}, errors.New("remote provider identity is invalid")
		}
		host = parsed.Hostname()
		remotePath = parsed.Path
	}
	remotePath = strings.Trim(strings.TrimSuffix(remotePath, ".git"), "/")
	parts := strings.Split(remotePath, "/")
	if host == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[0]+parts[1], "%") {
		return PreflightResult{}, errors.New("remote must identify an owner/repository")
	}
	return PreflightResult{RemoteHost: strings.ToLower(host), HostRepository: parts[0] + "/" + parts[1]}, nil
}

func verifyExpectedRemote(remoteURL, expectedIdentity, expectedHost, expectedRepository string) error {
	if expectedIdentity == "" {
		return errors.New("delivery request lacks the exact admitted remote identity")
	}
	identity, err := remoteIdentity(remoteURL)
	if err != nil || identity != expectedIdentity {
		return errors.New("delivery remote identity changed since preflight")
	}
	if expectedRepository == "" && expectedHost == "" {
		return nil
	}
	target, err := remoteProviderTarget(remoteURL)
	if err != nil || !strings.EqualFold(target.RemoteHost, expectedHost) || !strings.EqualFold(target.HostRepository, expectedRepository) {
		return errors.New("delivery remote identity changed since preflight")
	}
	return nil
}

func remoteIdentity(value string) (string, error) {
	if err := validateRemoteURL(value); err != nil {
		return "", err
	}
	var normalized string
	if filepath.IsAbs(value) {
		canonical, err := filepath.EvalSymlinks(value)
		if err != nil {
			return "", errors.New("delivery local remote cannot be canonicalized")
		}
		normalized = "file://" + filepath.ToSlash(canonical)
	} else if !strings.Contains(value, "://") {
		authority, remotePath, _ := strings.Cut(value, ":")
		user, host, _ := strings.Cut(authority, "@")
		normalized = "ssh-scp://" + user + "@" + strings.ToLower(host) + "/" + remotePath
	} else {
		parsed, _ := url.Parse(value)
		if parsed.Scheme == "file" {
			canonical, err := filepath.EvalSymlinks(parsed.Path)
			if err != nil {
				return "", errors.New("delivery local remote cannot be canonicalized")
			}
			normalized = "file://" + filepath.ToSlash(canonical)
		} else {
			userinfo := ""
			if parsed.User != nil {
				userinfo = parsed.User.Username() + "@"
			}
			normalized = strings.ToLower(parsed.Scheme) + "://" + userinfo + strings.ToLower(parsed.Host) + parsed.EscapedPath()
		}
	}
	digest := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("sha256:%x", digest[:]), nil
}

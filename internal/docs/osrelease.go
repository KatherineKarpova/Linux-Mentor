package docs

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Release struct {
	ID         string
	PrettyName string
}

func ReadRelease(path string) (Release, error) {
	file, err := os.Open(path)
	if err != nil {
		return Release{}, fmt.Errorf("open os-release file: %w", err)
	}
	defer file.Close()

	var release Release
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = parseReleaseValue(strings.TrimSpace(value))
		switch strings.TrimSpace(key) {
		case "ID":
			release.ID = strings.ToLower(value)
		case "PRETTY_NAME":
			release.PrettyName = value
		}
	}
	if err := scanner.Err(); err != nil {
		return Release{}, fmt.Errorf("read os-release file: %w", err)
	}
	if release.ID == "" {
		return Release{}, fmt.Errorf("os-release file %q has no ID", path)
	}
	for _, r := range release.ID {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '.' && r != '_' && r != '-' {
			return Release{}, fmt.Errorf("os-release file %q has invalid ID %q", path, release.ID)
		}
	}
	return release, nil
}

func parseReleaseValue(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	return value
}

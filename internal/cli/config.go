package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const maxConfigSize = 1 << 20

type config struct {
	Origin *url.URL
	Token  string
}

type configFile struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func loadConfig(path string) (config, *cliError) {
	file, err := openConfigFile(path)
	if err != nil {
		return config{}, configurationError()
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return config{}, configurationError()
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&^os.FileMode(0o600) != 0 {
		return config{}, configurationError()
	}

	limited := io.LimitReader(file, maxConfigSize+1)
	body, err := io.ReadAll(limited)
	if err != nil || len(body) > maxConfigSize {
		return config{}, configurationError()
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	var raw configFile
	if err := decoder.Decode(&raw); err != nil {
		return config{}, configurationError()
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return config{}, configurationError()
	}
	if raw.Token == "" {
		return config{}, configurationError()
	}
	origin, err := parseOrigin(raw.URL)
	if err != nil {
		return config{}, configurationError()
	}
	return config{Origin: origin, Token: raw.Token}, nil
}

func configPath(getenv func(string) string, userHomeDir func() (string, error)) (string, *cliError) {
	if path := getenv("VIKUNJA_CONFIG"); path != "" {
		return path, nil
	}
	home, err := userHomeDir()
	if err != nil || home == "" {
		return "", configurationError()
	}
	return filepath.Join(home, ".config", "vikunja", "config.json"), nil
}

func parseOrigin(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("empty origin")
	}
	origin, err := url.Parse(raw)
	if err != nil || origin.Opaque != "" || origin.User != nil || origin.Host == "" || origin.Hostname() == "" {
		return nil, errors.New("invalid origin")
	}
	if origin.Scheme != "http" && origin.Scheme != "https" {
		return nil, errors.New("invalid scheme")
	}
	if origin.Path != "" && origin.Path != "/" {
		return nil, errors.New("origin has path")
	}
	if origin.ForceQuery || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("origin has suffix")
	}
	if strings.HasSuffix(origin.Host, ":") {
		return nil, errors.New("invalid port")
	}
	if port := origin.Port(); port != "" {
		value, portErr := strconv.Atoi(port)
		if portErr != nil || value < 1 || value > 65535 {
			return nil, errors.New("invalid port")
		}
	}
	if _, err := url.ParseRequestURI(origin.RequestURI()); err != nil {
		return nil, errors.New("invalid origin")
	}
	origin.Path = ""
	origin.RawPath = ""
	return origin, nil
}

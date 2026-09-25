package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	apiRoot         = "/api/v2"
	maxResponseSize = 32 << 20
	userAgent       = "vikunja-cli/1"
)

func executeRequest(ctxOptions Options, cfg config, cmd command) (json.RawMessage, *cliError) {
	target, err := requestURL(cfg.Origin, cmd.Target)
	if err != nil {
		return nil, inputError()
	}
	request, err := http.NewRequestWithContext(ctxOptions.Context, cmd.Method, target.String(), bytes.NewReader(cmd.Body))
	if err != nil {
		return nil, inputError()
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+cfg.Token)
	request.Header.Set("User-Agent", userAgent)
	if cmd.ContentType != "" {
		request.Header.Set("Content-Type", cmd.ContentType)
	}

	client := constrainedClient(ctxOptions.HTTPClient, cfg.Origin)
	response, err := client.Do(request)
	if err != nil {
		return nil, transportError()
	}
	defer response.Body.Close()
	body, readErr := readBounded(response.Body)
	if readErr != nil {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, decodeAPIError(response.StatusCode, nil, cfg.Token)
		}
		return nil, protocolError()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, decodeAPIError(response.StatusCode, body, cfg.Token)
	}
	result, validationErr := validateSuccess(body, cmd.Shape)
	if validationErr != nil {
		return nil, validationErr
	}
	if jsonContainsToken(result, cfg.Token) {
		return nil, protocolError()
	}
	return result, nil
}

func constrainedClient(base *http.Client, origin *url.URL) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[0].Method == http.MethodDelete {
			return errors.New("destructive request redirect rejected")
		}
		if !sameOrigin(req.URL, origin) || !confinedAPIPath(req.URL) {
			return errors.New("redirect outside configured API")
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}
	return &client
}

func requestURL(origin *url.URL, target string) (*url.URL, error) {
	reference, err := url.Parse(target)
	if err != nil || reference.IsAbs() || reference.Host != "" || reference.Fragment != "" {
		return nil, errors.New("invalid target")
	}
	result := *origin
	result.Path = apiRoot + reference.Path
	result.RawPath = ""
	result.RawQuery = reference.RawQuery
	if !confinedAPIPath(&result) {
		return nil, errors.New("target outside API")
	}
	return &result, nil
}

func normalizeRawTarget(raw string) (string, error) {
	if raw == "" || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\r\n\x00") {
		return "", errors.New("invalid target")
	}
	reference, err := url.Parse(raw)
	if err != nil || reference.IsAbs() || reference.Host != "" || reference.User != nil || reference.Opaque != "" || reference.Fragment != "" {
		return "", errors.New("invalid target")
	}
	decoded, err := fullyUnescapePath(reference.Path)
	if err != nil {
		return "", err
	}
	decoded = strings.ReplaceAll(decoded, "\\", "/")
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("path traversal")
		}
	}
	if !strings.HasPrefix(decoded, "/") {
		decoded = "/" + decoded
	}
	cleaned := path.Clean(decoded)
	if cleaned == "." {
		cleaned = "/"
	}
	normalized := &url.URL{Path: cleaned, RawQuery: reference.RawQuery}
	return normalized.String(), nil
}

func confinedAPIPath(target *url.URL) bool {
	decoded, err := fullyUnescapePath(target.Path)
	if err != nil {
		return false
	}
	decoded = strings.ReplaceAll(decoded, "\\", "/")
	if decoded != apiRoot && !strings.HasPrefix(decoded, apiRoot+"/") {
		return false
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return path.Clean(decoded) == decoded
}

func fullyUnescapePath(raw string) (string, error) {
	decoded := raw
	for range len(raw) + 1 {
		next, err := url.PathUnescape(decoded)
		if err != nil {
			return "", errors.New("invalid escaping")
		}
		if next == decoded {
			return decoded, nil
		}
		decoded = next
	}
	return "", errors.New("excessive escaping")
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func readBounded(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseSize {
		return nil, errors.New("response too large")
	}
	return body, nil
}

func validateSuccess(body []byte, shape responseShape) (json.RawMessage, *cliError) {
	if len(bytes.TrimSpace(body)) == 0 {
		return json.RawMessage("null"), nil
	}
	raw, err := jsonValue(string(body))
	if err != nil {
		return nil, protocolError()
	}
	switch shape {
	case responseRaw:
		return raw, nil
	case responseList:
		if !validListEnvelope(raw) {
			return nil, protocolError()
		}
	case responseObject:
		if !isJSONObject(raw) {
			return nil, protocolError()
		}
	case responseOptional:
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && !isJSONObject(raw) {
			return nil, protocolError()
		}
	}
	return raw, nil
}

func isJSONObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(trimmed, &object) == nil && object != nil
}

func validListEnvelope(raw []byte) bool {
	var object map[string]json.RawMessage
	if !isJSONObject(raw) || json.Unmarshal(raw, &object) != nil {
		return false
	}
	items, ok := object["items"]
	if !ok || len(bytes.TrimSpace(items)) == 0 || bytes.TrimSpace(items)[0] != '[' {
		return false
	}
	var list []json.RawMessage
	if json.Unmarshal(items, &list) != nil {
		return false
	}
	for _, field := range []string{"total", "page", "per_page", "total_pages"} {
		value, exists := object[field]
		if !exists || !jsonInteger(value) {
			return false
		}
	}
	return true
}

func jsonInteger(raw []byte) bool {
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.ContainsAny(value, ".eE") {
		return false
	}
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

func decodeAPIError(status int, body []byte, token string) *cliError {
	kind, exitCode, message := "api", 6, "request rejected"
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		kind, exitCode, message = "authentication", 5, "authentication rejected"
	}
	result := &cliError{Kind: kind, Message: message, ExitCode: exitCode, HTTPStatus: status}
	var problem struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Code   *int   `json:"code"`
		Errors any    `json:"errors"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&problem) == nil {
		diagnostic := problem.Detail
		if diagnostic == "" {
			diagnostic = problem.Title
		}
		if diagnostic != "" {
			result.Message += ": " + sanitizeText(diagnostic, token)
		}
		result.Code = problem.Code
		if problem.Errors != nil {
			result.Details = sanitizeValue(problem.Errors, token)
		}
	}
	return result
}

func sanitizeText(value, token string) string {
	if token == "" {
		return value
	}
	return strings.ReplaceAll(value, token, "[redacted]")
}

func sanitizeValue(value any, token string) any {
	switch typed := value.(type) {
	case string:
		return sanitizeText(typed, token)
	case []any:
		for i := range typed {
			typed[i] = sanitizeValue(typed[i], token)
		}
	case map[string]any:
		cleaned := make(map[string]any, len(typed))
		for key, nested := range typed {
			cleanKey := key
			if token != "" {
				cleanKey = strings.ReplaceAll(key, token, "[redacted]")
			}
			cleaned[cleanKey] = sanitizeValue(nested, token)
		}
		return cleaned
	}
	return value
}

func jsonContainsToken(raw []byte, token string) bool {
	if token == "" {
		return false
	}
	if bytes.Contains(raw, []byte(token)) {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return false
	}
	return valueContainsToken(value, token)
}

func valueContainsToken(value any, token string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, token)
	case json.Number:
		return strings.Contains(string(typed), token)
	case []any:
		for _, nested := range typed {
			if valueContainsToken(nested, token) {
				return true
			}
		}
	case map[string]any:
		for key, nested := range typed {
			if strings.Contains(key, token) || valueContainsToken(nested, token) {
				return true
			}
		}
	}
	return false
}

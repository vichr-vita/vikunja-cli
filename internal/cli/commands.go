package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type responseShape int

const (
	responseObject responseShape = iota
	responseList
	responseRaw
	responseOptional
)

type command struct {
	Method      string
	Target      string
	Body        []byte
	ContentType string
	Shape       responseShape
}

type flagValues map[string]string

var (
	helpValue        = json.RawMessage(`{"usage":["vikunja-cli user get","vikunja-cli project list|get|create|update|delete","vikunja-cli task list|get|create|update|delete","vikunja-cli label list|get|create|update|delete|attach|detach","vikunja-cli comment list|get|create|update|delete","vikunja-cli api METHOD PATH"]}`)
	timestampPattern = regexp.MustCompile(`^([0-9]{4}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12][0-9]|3[01])T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9])(\.[0-9]+)?(Z|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])$`)
)

func parseCommand(args []string) (command, json.RawMessage, *cliError) {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return command{}, commandHelp(args), nil
		}
	}
	if len(args) < 2 {
		return command{}, nil, inputError()
	}

	resource, action, rest := args[0], args[1], args[2:]
	switch resource {
	case "user":
		if action != "get" || len(rest) != 0 {
			return command{}, nil, inputError()
		}
		return command{Method: http.MethodGet, Target: "/user", Shape: responseObject}, nil, nil
	case "project":
		return parseProject(action, rest)
	case "task":
		return parseTask(action, rest)
	case "label":
		return parseLabel(action, rest)
	case "comment":
		return parseComment(action, rest)
	case "api":
		return parseRaw(action, rest)
	default:
		return command{}, nil, inputError()
	}
}

func commandHelp(args []string) json.RawMessage {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "-h" && arg != "--help" {
			filtered = append(filtered, arg)
		}
	}
	usage, confirmation := destructiveHelp(filtered)
	if usage == "" {
		return helpValue
	}
	encoded, err := json.Marshal(map[string]any{
		"usage":        usage,
		"confirmation": confirmation,
	})
	if err != nil {
		return helpValue
	}
	return encoded
}

func destructiveHelp(args []string) (string, string) {
	if len(args) < 2 {
		return "", ""
	}
	if args[1] == "delete" && (args[0] == "project" || args[0] == "task" || args[0] == "label") {
		positional, _, err := parseFlags(args[2:], map[string]bool{"confirm": true})
		if err != nil || len(positional) != 1 {
			return "", ""
		}
		id, idErr := positiveID(positional[0])
		if idErr != nil {
			return "", ""
		}
		confirmation := args[0] + ":" + id
		return "vikunja-cli " + args[0] + " delete " + id + " --confirm " + confirmation, confirmation
	}
	if args[0] == "label" && args[1] == "detach" {
		positional, _, err := parseFlags(args[2:], map[string]bool{"confirm": true})
		if err != nil || len(positional) != 2 {
			return "", ""
		}
		ids, idErr := positiveIDs(positional)
		if idErr != nil {
			return "", ""
		}
		confirmation := "task:" + ids[0] + "/label:" + ids[1]
		return "vikunja-cli label detach " + ids[0] + " " + ids[1] + " --confirm " + confirmation, confirmation
	}
	if args[0] == "comment" && args[1] == "delete" {
		positional, _, err := parseFlags(args[2:], map[string]bool{"confirm": true})
		if err != nil || len(positional) != 2 {
			return "", ""
		}
		ids, idErr := positiveIDs(positional)
		if idErr != nil {
			return "", ""
		}
		confirmation := "task:" + ids[0] + "/comment:" + ids[1]
		return "vikunja-cli comment delete " + ids[0] + " " + ids[1] + " --confirm " + confirmation, confirmation
	}
	if args[0] == "api" && strings.EqualFold(args[1], http.MethodDelete) {
		positional, _, err := parseFlags(args[2:], map[string]bool{"confirm": true, "data": true})
		if err != nil || len(positional) != 1 {
			return "", ""
		}
		target, targetErr := normalizeRawTarget(positional[0])
		if targetErr != nil {
			return "", ""
		}
		confirmation := "DELETE:" + target
		return "vikunja-cli api DELETE " + target + " --confirm " + confirmation, confirmation
	}
	return "", ""
}

func parseProject(action string, args []string) (command, json.RawMessage, *cliError) {
	switch action {
	case "list":
		return parseListCommand("/projects", args)
	case "get":
		id, err := oneID(args, nil)
		if err != nil {
			return command{}, nil, err
		}
		return command{Method: http.MethodGet, Target: "/projects/" + id, Shape: responseObject}, nil, nil
	case "create":
		return parseWriteCommand(http.MethodPost, "/projects", args, 0, nil, false)
	case "update":
		return parseIDWriteCommand(http.MethodPatch, "/projects/", args, []ownedID{{Key: "id"}}, false)
	case "delete":
		return parseDeleteCommand("/projects/", "project:", args)
	default:
		return command{}, nil, inputError()
	}
}

func parseTask(action string, args []string) (command, json.RawMessage, *cliError) {
	switch action {
	case "list":
		return parseTaskList(args)
	case "get":
		id, err := oneID(args, nil)
		if err != nil {
			return command{}, nil, err
		}
		return command{Method: http.MethodGet, Target: "/tasks/" + id, Shape: responseObject}, nil, nil
	case "create":
		return parseTaskCreate(args)
	case "update":
		return parseIDWriteCommand(http.MethodPatch, "/tasks/", args, []ownedID{{Key: "id"}}, true)
	case "delete":
		return parseDeleteCommand("/tasks/", "task:", args)
	default:
		return command{}, nil, inputError()
	}
}

func parseLabel(action string, args []string) (command, json.RawMessage, *cliError) {
	switch action {
	case "list":
		return parseLabelList(args)
	case "get":
		id, err := oneID(args, nil)
		if err != nil {
			return command{}, nil, err
		}
		return command{Method: http.MethodGet, Target: "/labels/" + id, Shape: responseObject}, nil, nil
	case "create":
		return parseWriteCommand(http.MethodPost, "/labels", args, 0, nil, false)
	case "update":
		return parseIDWriteCommand(http.MethodPatch, "/labels/", args, []ownedID{{Key: "id"}}, false)
	case "delete":
		return parseDeleteCommand("/labels/", "label:", args)
	case "attach":
		positional, _, err := parseFlags(args, nil)
		if err != nil || len(positional) != 2 {
			return command{}, nil, inputError()
		}
		taskID, taskErr := positiveID(positional[0])
		labelID, labelErr := positiveID(positional[1])
		if taskErr != nil || labelErr != nil {
			return command{}, nil, inputError()
		}
		body := []byte(`{"label_id":` + labelID + `}`)
		return command{Method: http.MethodPost, Target: "/tasks/" + taskID + "/labels", Body: body, ContentType: "application/json", Shape: responseObject}, nil, nil
	case "detach":
		positional, flags, err := parseFlags(args, map[string]bool{"confirm": true})
		if err != nil || len(positional) != 2 {
			return command{}, nil, inputError()
		}
		taskID, taskErr := positiveID(positional[0])
		labelID, labelErr := positiveID(positional[1])
		if taskErr != nil || labelErr != nil || flags["confirm"] != "task:"+taskID+"/label:"+labelID {
			return command{}, nil, inputError()
		}
		return command{Method: http.MethodDelete, Target: "/tasks/" + taskID + "/labels/" + labelID, Shape: responseOptional}, nil, nil
	default:
		return command{}, nil, inputError()
	}
}

func parseComment(action string, args []string) (command, json.RawMessage, *cliError) {
	switch action {
	case "list":
		positional, flags, err := parseFlags(args, paginationFlagSet())
		if err != nil || len(positional) != 1 {
			return command{}, nil, inputError()
		}
		taskID, idErr := positiveID(positional[0])
		if idErr != nil {
			return command{}, nil, idErr
		}
		return listFromFlags("/tasks/"+taskID+"/comments", flags)
	case "get":
		ids, err := exactIDs(args, 2, nil)
		if err != nil {
			return command{}, nil, err
		}
		return command{Method: http.MethodGet, Target: "/tasks/" + ids[0] + "/comments/" + ids[1], Shape: responseObject}, nil, nil
	case "create":
		positional, flags, err := parseFlags(args, map[string]bool{"data": true})
		if err != nil || len(positional) != 1 {
			return command{}, nil, inputError()
		}
		taskID, idErr := positiveID(positional[0])
		if idErr != nil {
			return command{}, nil, idErr
		}
		body, bodyErr := objectData(flags, []ownedID{{Key: "task_id", Value: taskID}}, false)
		if bodyErr != nil {
			return command{}, nil, bodyErr
		}
		return writeCommand(http.MethodPost, "/tasks/"+taskID+"/comments", body), nil, nil
	case "update":
		positional, flags, err := parseFlags(args, map[string]bool{"data": true})
		if err != nil || len(positional) != 2 {
			return command{}, nil, inputError()
		}
		ids, idErr := positiveIDs(positional)
		if idErr != nil {
			return command{}, nil, idErr
		}
		body, bodyErr := objectData(flags, []ownedID{{Key: "task_id", Value: ids[0]}, {Key: "id", Value: ids[1]}}, false)
		if bodyErr != nil {
			return command{}, nil, bodyErr
		}
		cmd := writeCommand(http.MethodPatch, "/tasks/"+ids[0]+"/comments/"+ids[1], body)
		return cmd, nil, nil
	case "delete":
		positional, flags, err := parseFlags(args, map[string]bool{"confirm": true})
		if err != nil || len(positional) != 2 {
			return command{}, nil, inputError()
		}
		ids, idErr := positiveIDs(positional)
		if idErr != nil || flags["confirm"] != "task:"+ids[0]+"/comment:"+ids[1] {
			return command{}, nil, inputError()
		}
		return command{Method: http.MethodDelete, Target: "/tasks/" + ids[0] + "/comments/" + ids[1], Shape: responseOptional}, nil, nil
	default:
		return command{}, nil, inputError()
	}
}

func parseRaw(methodArg string, args []string) (command, json.RawMessage, *cliError) {
	positional, flags, err := parseFlags(args, map[string]bool{"data": true, "confirm": true})
	if err != nil || len(positional) != 1 {
		return command{}, nil, inputError()
	}
	method := strings.ToUpper(methodArg)
	if !validMethod(method) {
		return command{}, nil, inputError()
	}
	target, normalizeErr := normalizeRawTarget(positional[0])
	if normalizeErr != nil {
		return command{}, nil, inputError()
	}
	if method == http.MethodDelete && flags["confirm"] != "DELETE:"+target {
		return command{}, nil, inputError()
	}
	var body []byte
	if raw, exists := flags["data"]; exists {
		body, err = jsonValue(raw)
		if err != nil {
			return command{}, nil, inputError()
		}
	}
	contentType := ""
	if body != nil {
		contentType = "application/json"
	}
	return command{Method: method, Target: target, Body: body, ContentType: contentType, Shape: responseRaw}, nil, nil
}

func parseTaskList(args []string) (command, json.RawMessage, *cliError) {
	allowed := paginationFlagSet()
	allowed["project"] = true
	positional, flags, err := parseFlags(args, allowed)
	if err != nil || len(positional) != 0 {
		return command{}, nil, inputError()
	}
	target := "/tasks"
	if project, ok := flags["project"]; ok {
		id, idErr := positiveID(project)
		if idErr != nil {
			return command{}, nil, idErr
		}
		target = "/projects/" + id + "/tasks"
	}
	return listFromFlags(target, flags)
}

func parseTaskCreate(args []string) (command, json.RawMessage, *cliError) {
	positional, flags, err := parseFlags(args, map[string]bool{"data": true})
	if err != nil || len(positional) != 1 {
		return command{}, nil, inputError()
	}
	projectID, idErr := positiveID(positional[0])
	if idErr != nil {
		return command{}, nil, idErr
	}
	body, bodyErr := objectData(flags, []ownedID{{Key: "project_id", Value: projectID}}, true)
	if bodyErr != nil {
		return command{}, nil, bodyErr
	}
	return writeCommand(http.MethodPost, "/projects/"+projectID+"/tasks", body), nil, nil
}

func parseLabelList(args []string) (command, json.RawMessage, *cliError) {
	allowed := paginationFlagSet()
	allowed["task"] = true
	positional, flags, err := parseFlags(args, allowed)
	if err != nil || len(positional) != 0 {
		return command{}, nil, inputError()
	}
	target := "/labels"
	if task, ok := flags["task"]; ok {
		id, idErr := positiveID(task)
		if idErr != nil {
			return command{}, nil, idErr
		}
		target = "/tasks/" + id + "/labels"
	}
	return listFromFlags(target, flags)
}

func parseListCommand(target string, args []string) (command, json.RawMessage, *cliError) {
	allowed := paginationFlagSet()
	positional, flags, err := parseFlags(args, allowed)
	if err != nil || len(positional) != 0 {
		return command{}, nil, inputError()
	}
	return listFromFlags(target, flags)
}

func listFromFlags(target string, flags flagValues) (command, json.RawMessage, *cliError) {
	page, err := boundedInt(flags, "page", 1)
	if err != nil {
		return command{}, nil, inputError()
	}
	perPage, err := boundedInt(flags, "per-page", 50)
	if err != nil {
		return command{}, nil, inputError()
	}
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(perPage))
	if value, ok := flags["query"]; ok {
		query.Set("q", value)
	}
	return command{Method: http.MethodGet, Target: target + "?" + query.Encode(), Shape: responseList}, nil, nil
}

func parseIDWriteCommand(method, prefix string, args []string, owned []ownedID, timestamps bool) (command, json.RawMessage, *cliError) {
	positional, flags, err := parseFlags(args, map[string]bool{"data": true})
	if err != nil || len(positional) != 1 {
		return command{}, nil, inputError()
	}
	id, idErr := positiveID(positional[0])
	if idErr != nil {
		return command{}, nil, idErr
	}
	for i := range owned {
		if owned[i].Value == "" {
			owned[i].Value = id
		}
	}
	body, bodyErr := objectData(flags, owned, timestamps)
	if bodyErr != nil {
		return command{}, nil, bodyErr
	}
	return writeCommand(method, prefix+id, body), nil, nil
}

func parseWriteCommand(method, target string, args []string, positionalCount int, owned []ownedID, timestamps bool) (command, json.RawMessage, *cliError) {
	positional, flags, err := parseFlags(args, map[string]bool{"data": true})
	if err != nil || len(positional) != positionalCount {
		return command{}, nil, inputError()
	}
	body, bodyErr := objectData(flags, owned, timestamps)
	if bodyErr != nil {
		return command{}, nil, bodyErr
	}
	return writeCommand(method, target, body), nil, nil
}

func writeCommand(method, target string, body []byte) command {
	contentType := "application/json"
	if method == http.MethodPatch {
		contentType = "application/merge-patch+json"
	}
	return command{Method: method, Target: target, Body: body, ContentType: contentType, Shape: responseObject}
}

func parseDeleteCommand(prefix, confirmationPrefix string, args []string) (command, json.RawMessage, *cliError) {
	positional, flags, err := parseFlags(args, map[string]bool{"confirm": true})
	if err != nil || len(positional) != 1 {
		return command{}, nil, inputError()
	}
	id, idErr := positiveID(positional[0])
	if idErr != nil || flags["confirm"] != confirmationPrefix+id {
		return command{}, nil, inputError()
	}
	return command{Method: http.MethodDelete, Target: prefix + id, Shape: responseOptional}, nil, nil
}

func paginationFlagSet() map[string]bool {
	return map[string]bool{"page": true, "per-page": true, "query": true}
}

func parseFlags(args []string, allowed map[string]bool) ([]string, flagValues, error) {
	positional := make([]string, 0, len(args))
	flags := make(flagValues)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			positional = append(positional, arg)
			continue
		}
		nameValue := strings.TrimPrefix(arg, "--")
		name, value, hasValue := strings.Cut(nameValue, "=")
		if !allowed[name] || name == "" {
			return nil, nil, errors.New("unknown flag")
		}
		if _, duplicate := flags[name]; duplicate {
			return nil, nil, errors.New("duplicate flag")
		}
		if !hasValue {
			i++
			if i >= len(args) {
				return nil, nil, errors.New("missing flag value")
			}
			value = args[i]
		}
		flags[name] = value
	}
	return positional, flags, nil
}

func oneID(args []string, flags map[string]bool) (string, *cliError) {
	ids, err := exactIDs(args, 1, flags)
	if err != nil {
		return "", err
	}
	return ids[0], nil
}

func exactIDs(args []string, count int, flags map[string]bool) ([]string, *cliError) {
	positional, _, err := parseFlags(args, flags)
	if err != nil || len(positional) != count {
		return nil, inputError()
	}
	return positiveIDs(positional)
}

func positiveIDs(raw []string) ([]string, *cliError) {
	ids := make([]string, len(raw))
	for i, value := range raw {
		id, err := positiveID(value)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func positiveID(raw string) (string, *cliError) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return "", inputError()
	}
	return strconv.FormatInt(id, 10), nil
}

func boundedInt(flags flagValues, name string, fallback int) (int, error) {
	raw, ok := flags[name]
	if !ok {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 1000 {
		return 0, errors.New("out of range")
	}
	return value, nil
}

type ownedID struct {
	Key   string
	Value string
}

func objectData(flags flagValues, owned []ownedID, timestamps bool) ([]byte, *cliError) {
	raw, ok := flags["data"]
	if !ok {
		return nil, inputError()
	}
	value, err := jsonObject(raw)
	if err != nil {
		return nil, inputError()
	}
	for _, field := range owned {
		supplied, exists := value[field.Key]
		if !exists {
			continue
		}
		if !jsonIDEquals(supplied, field.Value) {
			return nil, inputError()
		}
		delete(value, field.Key)
	}
	if timestamps {
		for _, field := range []string{"due_date", "start_date", "end_date"} {
			if rawTimestamp, exists := value[field]; exists {
				normalized, normalizeErr := normalizeTimestamp(rawTimestamp)
				if normalizeErr != nil {
					return nil, inputError()
				}
				value[field] = normalized
			}
		}
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, inputError()
	}
	return body, nil
}

func jsonObject(raw string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace([]byte(raw))
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("not object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	var value map[string]json.RawMessage
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errors.New("invalid object")
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func jsonValue(raw string) ([]byte, error) {
	trimmed := bytes.TrimSpace([]byte(raw))
	if len(trimmed) == 0 {
		return nil, errors.New("empty JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func jsonIDEquals(raw json.RawMessage, want string) bool {
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return false
	}
	return string(number) == want
}

func normalizeTimestamp(raw json.RawMessage) (json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return json.RawMessage("null"), nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errors.New("timestamp must be string or null")
	}
	match := timestampPattern.FindStringSubmatch(value)
	if match == nil {
		return nil, errors.New("timestamp must be RFC3339")
	}
	parsed, err := time.Parse(time.RFC3339, match[1]+match[3])
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()
	normalized := utc.Format("2006-01-02T15:04:05") + match[2] + "Z"
	return json.Marshal(normalized)
}

func validMethod(method string) bool {
	if method == "" {
		return false
	}
	for _, r := range method {
		if r <= 32 || r >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
			return false
		}
	}
	return true
}

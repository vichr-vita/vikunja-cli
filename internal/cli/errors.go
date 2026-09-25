package cli

import (
	"bytes"
	"encoding/json"
	"io"
)

type cliError struct {
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	ExitCode   int    `json:"exit_code"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Code       *int   `json:"code,omitempty"`
	Details    any    `json:"details,omitempty"`
}

func (e *cliError) Error() string { return e.Message }

func inputError() *cliError {
	return &cliError{Kind: "input", Message: "invalid command input", ExitCode: 2}
}

func configurationError() *cliError {
	return &cliError{Kind: "configuration", Message: "invalid configuration", ExitCode: 3}
}

func transportError() *cliError {
	return &cliError{Kind: "transport", Message: "request failed", ExitCode: 4}
}

func protocolError() *cliError {
	return &cliError{Kind: "protocol", Message: "invalid server response", ExitCode: 7}
}

func internalError() *cliError {
	return &cliError{Kind: "internal", Message: "internal failure", ExitCode: 1}
}

func encodeJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := json.NewEncoder(&output).Encode(value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeJSON(w io.Writer, value any) error {
	encoded, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = w.Write(encoded)
	return err
}

func writeCLIError(w io.Writer, err *cliError) {
	_ = writeJSON(w, struct {
		Error *cliError `json:"error"`
	}{Error: err})
}

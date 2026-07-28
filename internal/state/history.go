package state

import (
	"encoding/json"
	"errors"
	"path/filepath"
)

// History is the picker-facing recency list view of FocusMRU.
type History struct {
	Workspaces []string `json:"workspaces"`
}

const maxWorkspaces = 50

func Path(dir string) string { return filepath.Join(dir, mruFile) }

func dedupeWorkspaces(front []string, rest []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(front)+len(rest))
	for _, id := range append(front, rest...) {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == maxWorkspaces {
			return out
		}
	}
	return out
}

func isJSONDecodeError(err error) bool {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &syntaxErr) || errors.As(err, &typeErr)
}

package config

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ParseConfigValue parses a string into a typed value: bool, int, JSON object/array, or string fallback.
func ParseConfigValue(s string) any {
	lower := strings.ToLower(s)
	if lower == "true" {
		return true
	}
	if lower == "false" {
		return false
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	var jsonVal any
	if err := json.Unmarshal([]byte(s), &jsonVal); err == nil {
		return jsonVal
	}
	return s
}

// GetDotPath retrieves a nested value from map using dot notation (e.g. "agents.agy.submit_key").
func GetDotPath(m map[string]any, dotPath string) any {
	keys := strings.Split(dotPath, ".")
	var curr any = m
	for _, k := range keys {
		currMap, ok := curr.(map[string]any)
		if !ok {
			return nil
		}
		val, exists := currMap[k]
		if !exists {
			return nil
		}
		curr = val
	}
	return curr
}

// SetDotPath sets a nested value in map using dot notation, creating intermediate maps as needed.
func SetDotPath(m map[string]any, dotPath string, val any) {
	keys := strings.Split(dotPath, ".")
	curr := m
	for _, k := range keys[:len(keys)-1] {
		next, ok := curr[k].(map[string]any)
		if !ok {
			next = make(map[string]any)
			curr[k] = next
		}
		curr = next
	}
	curr[keys[len(keys)-1]] = val
}

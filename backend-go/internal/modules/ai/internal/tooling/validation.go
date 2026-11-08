package tooling

import (
	"fmt"
	"math"
	"reflect"
)

func validate(schema map[string]any, v any) error {
	switch schema["type"] {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object")
		}
		p, _ := schema["properties"].(map[string]any)
		required := []string{}
		switch r := schema["required"].(type) {
		case []string:
			required = r
		case []any:
			for _, x := range r {
				required = append(required, x.(string))
			}
		}
		for _, k := range required {
			if _, ok := m[k]; !ok {
				return fmt.Errorf("missing %s", k)
			}
		}
		for k, x := range m {
			s, ok := p[k].(map[string]any)
			if !ok {
				if schema["additionalProperties"] == false {
					return fmt.Errorf("unknown %s", k)
				}
				continue
			}
			if err := validate(s, x); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
	case "string":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected string")
		}
		if n, ok := number(schema["minLength"]); ok && float64(len([]rune(s))) < n {
			return fmt.Errorf("string too short")
		}
		if n, ok := number(schema["maxLength"]); ok && float64(len([]rune(s))) > n {
			return fmt.Errorf("string too long")
		}
	case "integer", "number":
		n, ok := number(v)
		if !ok || schema["type"] == "integer" && math.Trunc(n) != n {
			return fmt.Errorf("expected number")
		}
		if x, ok := number(schema["minimum"]); ok && n < x {
			return fmt.Errorf("below minimum")
		}
		if x, ok := number(schema["maximum"]); ok && n > x {
			return fmt.Errorf("above maximum")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "array":
		r := reflect.ValueOf(v)
		if !r.IsValid() || (r.Kind() != reflect.Slice && r.Kind() != reflect.Array) {
			return fmt.Errorf("expected array")
		}
		if n, ok := number(schema["minItems"]); ok && float64(r.Len()) < n {
			return fmt.Errorf("too few items")
		}
		if n, ok := number(schema["maxItems"]); ok && float64(r.Len()) > n {
			return fmt.Errorf("too many items")
		}
		if s, ok := schema["items"].(map[string]any); ok {
			for i := 0; i < r.Len(); i++ {
				if err := validate(s, r.Index(i).Interface()); err != nil {
					return err
				}
			}
		}
	}
	if e, ok := schema["enum"]; ok {
		r := reflect.ValueOf(e)
		found := false
		for i := 0; i < r.Len(); i++ {
			if reflect.DeepEqual(r.Index(i).Interface(), v) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("invalid enum")
		}
	}
	return nil
}
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

package prompts

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// allowedUI controls which x-aihub-ui widgets are accepted.
var allowedUI = map[string]bool{
	"text": true, "textarea": true, "markdown": true, "code": true,
	"number": true, "switch": true, "select": true, "radio": true,
	"multi-select": true, "model-provider": true, "model-name": true,
	"file": true, "image": true, "effect-file": true,
	"group": true, "repeatable-group": true,
}

// allowedTypes is the JSON Schema subset supported by AIHub.
var allowedTypes = map[string]bool{
	"string": true, "number": true, "integer": true, "boolean": true,
	"array": true, "object": true,
}

// variablePattern matches {{name}} template variables.
var variablePattern = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.\-]+)\s*\}\}`)

// ValidateSchema checks a JSON Schema subset with x-aihub-* extensions.
func ValidateSchema(schema map[string]any) error {
	if schema == nil {
		return fmt.Errorf("schema 不能为空")
	}
	if t, _ := schema["type"].(string); t != "object" {
		return fmt.Errorf("schema 顶层 type 必须是 object")
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return fmt.Errorf("schema 必须包含 properties")
	}
	if err := validateProperties(props, ""); err != nil {
		return err
	}
	if reqs, ok := schema["required"].([]any); ok {
		for _, r := range reqs {
			name, _ := r.(string)
			if _, ok := props[name]; !ok {
				return fmt.Errorf("required 字段 %q 不在 properties 中", name)
			}
		}
	}
	// Validate declared variables.
	if vs, ok := schema["x-aihub-variables"].([]any); ok {
		seen := map[string]bool{}
		for _, v := range vs {
			vm, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("x-aihub-variables 每项必须是对象")
			}
			name, _ := vm["name"].(string)
			if name == "" || seen[name] {
				return fmt.Errorf("x-aihub-variables 的 name 必须唯一且非空")
			}
			seen[name] = true
		}
	}
	return nil
}

func validateProperties(props map[string]any, path string) error {
	for name, raw := range props {
		p, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s%s: 属性必须是对象", path, name)
		}
		typ, _ := p["type"].(string)
		if typ == "" {
			return fmt.Errorf("%s%s: 缺少 type", path, name)
		}
		if !allowedTypes[typ] {
			return fmt.Errorf("%s%s: 不支持的 type %q", path, name, typ)
		}
		if ui, _ := p["x-aihub-ui"].(string); ui != "" && !allowedUI[ui] {
			return fmt.Errorf("%s%s: 不支持的 x-aihub-ui %q", path, name, ui)
		}
		switch typ {
		case "array":
			if ui, _ := p["x-aihub-ui"].(string); ui != "" && ui != "multi-select" && ui != "repeatable-group" {
				return fmt.Errorf("%s%s: array 类型仅支持 multi-select 或 repeatable-group", path, name)
			}
			if items, ok := p["items"].(map[string]any); ok {
				it, _ := items["type"].(string)
				if it == "object" {
					iprops, _ := items["properties"].(map[string]any)
					if iprops == nil {
						return fmt.Errorf("%s%s: 重复组 items 必须包含 properties", path, name)
					}
					if err := validateProperties(iprops, path+name+"."); err != nil {
						return err
					}
				} else if it == "" {
					return fmt.Errorf("%s%s: items 缺少 type", path, name)
				}
			}
		case "object":
			if ui, _ := p["x-aihub-ui"].(string); ui != "" && ui != "group" {
				return fmt.Errorf("%s%s: object 类型仅支持 group", path, name)
			}
			if iprops, ok := p["properties"].(map[string]any); ok {
				if err := validateProperties(iprops, path+name+"."); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// SchemaVars returns the declared x-aihub-variables of a schema.
func SchemaVars(schema map[string]any) []map[string]any {
	out := []map[string]any{}
	if vs, ok := schema["x-aihub-variables"].([]any); ok {
		for _, v := range vs {
			if vm, ok := v.(map[string]any); ok {
				out = append(out, vm)
			}
		}
	}
	return out
}

// ValidateContent checks content against the schema's constraints.
func ValidateContent(schema, content map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return fmt.Errorf("schema 无效")
	}
	return validateObjectContent(props, content, "")
}

func validateObjectContent(props map[string]any, content map[string]any, path string) error {
	// required check
	if err := checkRequired(props, content, path); err != nil {
		return err
	}
	for name, raw := range props {
		p, _ := raw.(map[string]any)
		val, exists := content[name]
		if !exists || val == nil {
			continue
		}
		typ, _ := p["type"].(string)
		switch typ {
		case "string":
			s, ok := val.(string)
			if !ok {
				return fmt.Errorf("%s%s 必须是字符串", path, name)
			}
			if err := checkString(p, s, path+name); err != nil {
				return err
			}
		case "number", "integer":
			f, ok := asFloat(val)
			if !ok {
				return fmt.Errorf("%s%s 必须是数字", path, name)
			}
			if err := checkNumber(p, f, path+name); err != nil {
				return err
			}
		case "boolean":
			if _, ok := val.(bool); !ok {
				return fmt.Errorf("%s%s 必须是布尔值", path, name)
			}
		case "array":
			arr, ok := val.([]any)
			if !ok {
				return fmt.Errorf("%s%s 必须是数组", path, name)
			}
			if items, ok := p["items"].(map[string]any); ok {
				it, _ := items["type"].(string)
				if it == "object" {
					iprops, _ := items["properties"].(map[string]any)
					for i, item := range arr {
						obj, ok := item.(map[string]any)
						if !ok {
							return fmt.Errorf("%s%s[%d] 必须是对象", path, name, i)
						}
						if err := validateObjectContent(iprops, obj, fmt.Sprintf("%s%s[%d].", path, name, i)); err != nil {
							return err
						}
					}
				} else if it == "string" {
					for _, item := range arr {
						if _, ok := item.(string); !ok {
							return fmt.Errorf("%s%s 数组元素必须是字符串", path, name)
						}
					}
				}
			}
		case "object":
			obj, ok := val.(map[string]any)
			if !ok {
				return fmt.Errorf("%s%s 必须是对象", path, name)
			}
			if iprops, ok := p["properties"].(map[string]any); ok {
				if err := validateObjectContent(iprops, obj, path+name+"."); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkRequired(props, content map[string]any, path string) error {
	for name, raw := range props {
		p, _ := raw.(map[string]any)
		if req, _ := p["required"].(bool); req {
			v, ok := content[name]
			if !ok || v == nil || v == "" || v == false {
				return fmt.Errorf("%s%s 为必填", path, name)
			}
		}
	}
	return nil
}

func checkString(p map[string]any, s, path string) error {
	if min, ok := p["minLength"].(float64); ok && float64(len([]rune(s))) < min {
		return fmt.Errorf("%s 长度不能小于 %d", path, int(min))
	}
	if max, ok := p["maxLength"].(float64); ok && float64(len([]rune(s))) > max {
		return fmt.Errorf("%s 长度不能大于 %d", path, int(max))
	}
	if enum, ok := p["enum"].([]any); ok {
		matched := false
		for _, e := range enum {
			if es, ok := e.(string); ok && es == s {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s 不在允许的枚举值中", path)
		}
	}
	return nil
}

func checkNumber(p map[string]any, f float64, path string) error {
	if min, ok := p["minimum"].(float64); ok && f < min {
		return fmt.Errorf("%s 不能小于 %v", path, min)
	}
	if max, ok := p["maximum"].(float64); ok && f > max {
		return fmt.Errorf("%s 不能大于 %v", path, max)
	}
	return nil
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// ExtractVariables returns the sorted set of {{var}} names used in a string.
func ExtractVariables(s string) []string {
	seen := map[string]bool{}
	for _, m := range variablePattern.FindAllStringSubmatch(s, -1) {
		seen[strings.TrimSpace(m[1])] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidateVariables checks that every variable used in content strings is
// declared in the schema and returns the used variable names.
func ValidateVariables(schema map[string]any, content map[string]any) ([]string, error) {
	declared := map[string]bool{}
	for _, v := range SchemaVars(schema) {
		if name, _ := v["name"].(string); name != "" {
			declared[name] = true
		}
	}
	used := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch val := v.(type) {
		case string:
			for _, name := range ExtractVariables(val) {
				used[name] = true
			}
		case []any:
			for _, item := range val {
				walk(item)
			}
		case map[string]any:
			for _, item := range val {
				walk(item)
			}
		}
	}
	walk(content)
	missing := []string{}
	for name := range used {
		if !declared[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return nil, fmt.Errorf("使用了未声明的变量: %s", strings.Join(missing, ", "))
	}
	usedList := make([]string, 0, len(used))
	for name := range used {
		usedList = append(usedList, name)
	}
	sort.Strings(usedList)
	return usedList, nil
}

// RenderTemplate replaces {{name}} variables in s using values.
func RenderTemplate(s string, values map[string]any) string {
	return variablePattern.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(m, "}}"), "{{"))
		v, ok := values[name]
		if !ok {
			return m
		}
		switch val := v.(type) {
		case string:
			return val
		case json.Number:
			return val.String()
		default:
			b, err := json.Marshal(val)
			if err != nil {
				return m
			}
			return string(b)
		}
	})
}

// RenderContent renders all string leaves of content with values.
func RenderContent(content, values map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range content {
		out[k] = renderValue(v, values)
	}
	return out
}

func renderValue(v any, values map[string]any) any {
	switch val := v.(type) {
	case string:
		return RenderTemplate(val, values)
	case []any:
		arr := make([]any, len(val))
		for i, item := range val {
			arr[i] = renderValue(item, values)
		}
		return arr
	case map[string]any:
		m := map[string]any{}
		for k, item := range val {
			m[k] = renderValue(item, values)
		}
		return m
	default:
		return v
	}
}

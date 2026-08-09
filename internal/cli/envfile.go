package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// EnvFileSecretSource reads secret values from a local KEY=VALUE file without
// storing them on the server.
type EnvFileSecretSource struct {
	values map[string]string
}

// NewEnvFileSecretSource parses a .env-style file.
func NewEnvFileSecretSource(path string) (*EnvFileSecretSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	values := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		values[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return &EnvFileSecretSource{values: values}, nil
}

// Prompt implements SecretSource.
func (e *EnvFileSecretSource) Prompt(name, description string) (string, error) {
	if v, ok := e.values[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("环境变量文件缺少 %s", name)
}

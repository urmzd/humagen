package httpapi

import (
	"fmt"
	"testing"

	"go.yaml.in/yaml/v4"
)

// Go 1.27 accepts interface-keyed maps when marshaling JSON, while the
// supported Go 1.26 toolchain does not. Keep the source contract portable.
func specJSONKeys(value any, path string) error {
	switch node := value.(type) {
	case map[any]any:
		for key, child := range node {
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("%s has non-string key %v (%T); quote YAML response codes", path, key, key)
			}
			if err := specJSONKeys(child, path+"."+name); err != nil {
				return err
			}
		}
	case map[string]any:
		for name, child := range node {
			if err := specJSONKeys(child, path+"."+name); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range node {
			if err := specJSONKeys(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestSpecRejectsUnquotedResponseCodes(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want bool
	}{
		{"responses:\n  200:\n    description: ok\n", false},
		{"responses:\n  \"200\":\n    description: ok\n", true},
	} {
		var value any
		if err := yaml.Unmarshal([]byte(test.raw), &value); err != nil {
			t.Fatal(err)
		}
		if err := specJSONKeys(value, "openapi"); (err == nil) != test.want {
			t.Fatalf("mapping portability: raw=%q err=%v", test.raw, err)
		}
	}
}

package loops

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/wins/jaz/backend/internal/reasoning"
)

// A routine stores whatever level a picker offered, so its tool schema has to
// accept the whole vocabulary. The two lists drifting apart is what refused pi's
// "off" at creation while the picker still offered it.
func TestLoopSchemaAcceptsEveryStoredLevel(t *testing.T) {
	schemas := map[string]*jsonschema.Schema{
		"create": effortSchema[MCPCreateInput](),
		"update": effortSchema[MCPUpdateInput](),
	}
	for name, schema := range schemas {
		field, err := schema.Properties["reasoning_effort"].Resolve(nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, level := range reasoning.Levels {
			if err := field.Validate(level); err != nil {
				t.Fatalf("%s schema refused stored level %q: %v", name, level, err)
			}
		}
		if err := field.Validate("turbo"); err == nil {
			t.Fatalf("%s schema accepted a level outside the vocabulary", name)
		}
	}
}

// The schema reaches clients as JSON, and an enum of untyped values is what
// makes a level selectable rather than a free-typed string.
func TestLoopSchemaPublishesTheEffortEnum(t *testing.T) {
	encoded, err := json.Marshal(effortSchema[MCPCreateInput]())
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Enum []any `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	if got, want := len(schema.Properties["reasoning_effort"].Enum), len(reasoning.Levels)+1; got != want {
		t.Fatalf("reasoning_effort enum has %d values, want %d: %s", got, want, encoded)
	}
}

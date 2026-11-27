package acp

import (
	"encoding/json"
	"io"
	"os"
	"slices"
	"testing"

	"github.com/wins/jaz/backend/internal/modelcatalog"
)

// devinModelCatalogProcess is a stand-in for `devin acp` that advertises its
// thought levels per model, as Devin does: a session only describes the levels
// of the model it runs.
func TestDevinModelCatalogProcess(t *testing.T) {
	devinModelCatalogProcess(t)
}

func devinModelCatalogProcess(t *testing.T) {
	root := os.Getenv("JAZ_TEST_DEVIN_CATALOG")
	if root == "" {
		return
	}
	model := "native-basic"
	choices := []map[string]string{
		{"value": "native-basic", "name": "Native Basic"},
		{"value": "native-wide", "name": "Native Wide"},
		{"value": "native-plain", "name": "Native Plain"},
	}
	levels := map[string][]string{
		"native-basic": {"medium", "high", "max"},
		"native-wide":  {"low", "medium", "high", "xhigh", "max"},
	}
	response := func() map[string]any {
		options := []map[string]any{{"id": "model", "category": "model", "type": "select", "currentValue": model, "options": choices}}
		if values := levels[model]; len(values) > 0 {
			entries := make([]map[string]string, 0, len(values))
			for _, value := range values {
				entries = append(entries, map[string]string{"value": value, "name": value})
			}
			options = append(options, map[string]any{"id": "thought_level", "category": "thought_level", "type": "select", "currentValue": values[0], "options": entries})
		}
		return map[string]any{"configOptions": options}
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := decoder.Decode(&req); err == io.EOF {
			os.Exit(0)
		} else if err != nil {
			t.Fatal(err)
		}
		result := map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1}
		case "session/new":
			result = response()
			result["sessionId"] = "devin-catalog-probe"
		case "session/set_config_option":
			var params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatal(err)
			}
			if params.ConfigID == "model" {
				model = params.Value
			}
			result = response()
		case "session/close":
			result = map[string]any{}
			if err := os.WriteFile(root+"/closed", nil, 0o600); err != nil {
				t.Fatal(err)
			}
		default:
			result = map[string]any{}
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDevinModelDiscoveryFollowsEachModel(t *testing.T) {
	root := t.TempDir()
	clearHostEnv(t)
	manager := NewManager(nil, Config{Root: root, AgentSource: AgentCatalog{AgentDevin: {
		Command: os.Args[0], Args: []string{"-test.run=^TestDevinModelCatalogProcess$"},
		Env: map[string]string{"JAZ_TEST_DEVIN_CATALOG": root},
	}}}, nil)
	t.Cleanup(manager.Close)
	catalog, err := manager.DiscoverModels(t.Context(), AgentDevin)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 3 || catalog.DefaultModel != "native-basic" {
		t.Fatalf("catalog = %#v", catalog)
	}
	basic, wide, plain := catalog.Models[0], catalog.Models[1], catalog.Models[2]
	if basic.Value != "native-basic" || !slices.Equal(basic.Reasoning.Efforts, []string{"medium", "high", "max"}) {
		t.Fatalf("first model's levels: %#v", basic)
	}
	if wide.Value != "native-wide" || !slices.Equal(wide.Reasoning.Efforts, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Fatalf("a model's own levels were replaced by another model's: %#v", wide)
	}
	if plain.Value != "native-plain" || plain.Reasoning.Status != modelcatalog.ReasoningUnavailable || len(plain.Reasoning.Efforts) != 0 {
		t.Fatalf("invented levels for a model that advertises none: %#v", plain)
	}
	if _, err := os.Stat(root + "/closed"); err != nil {
		t.Fatalf("discovery session was not closed: %v", err)
	}
}

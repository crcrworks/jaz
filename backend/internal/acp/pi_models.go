package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	modelprovider "github.com/wins/jaz/backend/internal/provider"
	"github.com/wins/jaz/backend/internal/runtimefiles"
)

const (
	piProviderAPI  = "openai-completions"
	piModelListTTL = 10 * time.Minute
)

type piModelsFile struct {
	Providers map[string]piProviderConfig `json:"providers"`
}

type piProviderConfig struct {
	Name    string            `json:"name,omitempty"`
	BaseURL string            `json:"baseUrl,omitempty"`
	API     string            `json:"api,omitempty"`
	APIKey  string            `json:"apiKey,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Models are pi's own model entries. An entry the profile already declares
	// is carried over verbatim, so nothing written there is lost.
	Models []any `json:"models,omitempty"`
}

// piModel is the model entry Jaz adds itself: what pi shows for a model it is
// told about, and nothing else — thinking metadata is the profile's own word.
type piModel struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
}

// PreparePiProfile points Jaz's isolated pi profile at Jaz's Model Providers:
// providers pi does not describe itself are written to <profile>/models.json,
// and their keys are bound into env under the variable models.json names, so no
// secret reaches disk. A provider stays declared without a key, because pi's
// own login needs the declaration too — the stored credential wins over it. A
// built-in provider pi already describes is never declared: one entry would
// replace pi's own api and model list with a narrower guess. It returns the
// providers Jaz handed pi a key for.
func PreparePiProfile(root string, env map[string]string, providers map[string]modelprovider.ModelProviderConfig, model string) ([]string, error) {
	home := runtimefiles.New(root).ACPPiHome
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("prepare pi profile %s: %w", home, err)
	}
	selectedProvider, _ := modelprovider.SplitProviderModel(model)
	path := filepath.Join(home, "models.json")
	current, readErr := os.ReadFile(path)
	file := piModelsFile{Providers: map[string]piProviderConfig{}}
	if readErr == nil {
		_ = json.Unmarshal(current, &file)
	}
	declared := piModelsFile{Providers: map[string]piProviderConfig{}}
	supplied := []string{}
	for _, resolved := range modelprovider.ResolveModelProviders(providers) {
		keyEnv := modelprovider.ConfiguredAPIKeyEnv(resolved.ID, resolved.Config)
		key := strings.TrimSpace(resolved.Config.APIKey)
		if key == "" && keyEnv != "" {
			key = modelProviderKeyValue(root, env, keyEnv)
		}
		if key != "" && keyEnv != "" {
			// pi reads a provider's key from its own variable, whether pi
			// describes the provider or Jaz declares it below, so the key is
			// bound for both. models.json only ever names the variable.
			env[keyEnv] = key
			supplied = append(supplied, resolved.ID)
		}
		if resolved.BuiltIn && !resolved.Meta.OpenAICompatible {
			// pi carries this provider's api and model list itself; a models.json
			// entry would replace them with a narrower copy.
			continue
		}
		if !resolved.Meta.SupportsCapability(modelprovider.CapabilityChatCompletions) {
			continue
		}
		baseURL := firstNonEmpty(resolved.Config.BaseURL, resolved.Meta.BaseURL)
		// Without a key the endpoint would refuse the list, so Jaz asks only
		// when it can authenticate; anything else would probe a provider on
		// every Settings read.
		models := piProviderModels(piModelSource{Provider: resolved.ID, BaseURL: baseURL, Key: key}, resolved.Meta.DefaultModel, selectedProvider, model)
		if baseURL == "" || len(models) == 0 {
			continue
		}
		apiKey := ""
		switch {
		case key != "" && keyEnv != "":
			apiKey = "$" + keyEnv
		case !resolved.Meta.RequiresAPIKey:
			// pi needs a key present to offer the model; an endpoint that does
			// not authenticate ignores the dummy value pi documents.
			apiKey = "none"
		}
		declared.Providers[resolved.ID] = piProviderConfig{
			Name:    firstNonEmpty(resolved.Config.Label, resolved.Meta.Label),
			BaseURL: baseURL,
			API:     piProviderAPI,
			APIKey:  apiKey,
			Headers: resolved.Config.Headers,
			Models:  withDeclaredModels(file.Providers[resolved.ID].Models, models),
		}
	}
	for id, provider := range declared.Providers {
		file.Providers[id] = provider
	}
	if len(declared.Providers) > 0 {
		data, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			return nil, err
		}
		if readErr != nil || !bytes.Equal(current, data) {
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return nil, fmt.Errorf("write pi models %s: %w", path, err)
			}
		}
	}
	sort.Strings(supplied)
	return supplied, nil
}

// withDeclaredModels keeps what the profile already says about a model Jaz is
// declaring, verbatim, and keeps a model only the profile lists: pi reads an
// entry without reasoning as a model that cannot think, so replacing one with
// the id and context window Jaz knows turned a model pi runs at six levels into
// one it runs at none. Jaz adds models; it never removes one.
func withDeclaredModels(kept []any, declared []any) []any {
	byID := make(map[string]any, len(kept))
	for _, entry := range kept {
		if id := piModelID(entry); id != "" {
			byID[id] = entry
		}
	}
	out := make([]any, 0, len(kept)+len(declared))
	listed := make(map[string]struct{}, len(declared))
	for _, entry := range declared {
		id := piModelID(entry)
		listed[id] = struct{}{}
		if known, ok := byID[id]; ok {
			entry = known
		}
		out = append(out, entry)
	}
	for _, entry := range kept {
		if _, ok := listed[piModelID(entry)]; !ok {
			out = append(out, entry)
		}
	}
	return out
}

func piModelID(entry any) string {
	raw, err := json.Marshal(entry)
	if err != nil {
		return ""
	}
	var model piModel
	if json.Unmarshal(raw, &model) != nil {
		return ""
	}
	return model.ID
}

// piProviderModels is the model list pi may run for a provider Jaz supplies:
// what the provider advertises plus its default and the thread's selection, in
// pi's provider-relative id. pi refuses a model its catalog does not list.
func piProviderModels(source piModelSource, defaultModel, selectedProvider, selected string) []any {
	ids := []string{strings.TrimSpace(defaultModel)}
	if selectedProvider == source.Provider {
		_, id := modelprovider.SplitProviderModel(selected)
		ids = append(ids, id)
	}
	var models []piModel
	if source.Key != "" {
		models = piListModels(source)
	}
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		seen[model.ID] = true
	}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, piModel{ID: id, Name: id})
		}
	}
	out := make([]any, 0, len(models))
	for _, model := range models {
		out = append(out, model)
	}
	return out
}

// piModelSource is the endpoint a provider's model list comes from, and the key
// that endpoint needs.
type piModelSource struct {
	Provider string
	BaseURL  string
	Key      string
}

func (s piModelSource) cacheKey() string {
	return s.Provider + " " + strings.TrimRight(s.BaseURL, "/")
}

var piModelLists = struct {
	sync.Mutex
	byProvider map[string]piModelList
}{byProvider: map[string]piModelList{}}

type piModelList struct {
	models  []piModel
	fetched time.Time
}

// piListModels asks an OpenAI-compatible provider which models it serves. Auth
// probes run this on every Settings read, so a fresh answer is reused; a stale
// one is returned only when the provider cannot be reached now.
func piListModels(source piModelSource) []piModel {
	key := source.cacheKey()
	piModelLists.Lock()
	cached, ok := piModelLists.byProvider[key]
	piModelLists.Unlock()
	if ok && time.Since(cached.fetched) < piModelListTTL {
		return cached.models
	}
	models := fetchPiModelList(source)
	if models == nil {
		// A failed fetch must not slide the cache forward: keeping the original
		// timestamp lets a transient outage heal once the TTL passes.
		return cached.models
	}
	piModelLists.Lock()
	piModelLists.byProvider[key] = piModelList{models: models, fetched: time.Now()}
	piModelLists.Unlock()
	return models
}

func fetchPiModelList(source piModelSource) []piModel {
	endpoint, err := modelprovider.ModelsURL(source.BaseURL)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	if source.Key != "" {
		req.Header.Set("Authorization", "Bearer "+source.Key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		Data []struct {
			ID                 string   `json:"id"`
			Name               string   `json:"name"`
			ContextLength      int      `json:"context_length"`
			SupportedEndpoints []string `json:"supported_endpoints"`
		} `json:"data"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&body) != nil {
		return nil
	}
	models := []piModel{}
	for _, entry := range body.Data {
		if entry.ID == "" || (len(entry.SupportedEndpoints) > 0 && !slices.Contains(entry.SupportedEndpoints, "/chat/completions")) {
			continue
		}
		models = append(models, piModel{ID: entry.ID, Name: firstNonEmpty(entry.Name, entry.ID), ContextWindow: entry.ContextLength})
	}
	return models
}

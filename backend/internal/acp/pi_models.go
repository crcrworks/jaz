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

// piModelsFile is what Jaz keeps in <profile>/models.json. JazModels names, per
// provider, the models Jaz added: the ones it must keep current from the
// provider's own list. JazThinking names the ones Jaz called thinking, so a
// declaration withdrawn there stops claiming a model thinks while the reasoning
// someone wrote themselves stays. Everything else in the file was declared by a
// person, and Jaz reads it without rewriting it.
type piModelsFile struct {
	Providers   map[string]piProviderConfig `json:"providers"`
	JazModels   map[string][]string         `json:"jazModels,omitempty"`
	JazThinking map[string][]string         `json:"jazThinking,omitempty"`
}

type piProviderConfig struct {
	Name    string            `json:"name,omitempty"`
	BaseURL string            `json:"baseUrl,omitempty"`
	API     string            `json:"api,omitempty"`
	APIKey  string            `json:"apiKey,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Models are pi's own model entries. One Jaz did not add is carried over as
	// it is, since pi reads an entry without reasoning as a model that cannot
	// think and Jaz will not replace what it was told.
	Models []map[string]any `json:"models,omitempty"`
}

// piModel is the model entry Jaz adds itself: what pi shows for a model it is
// told about, and nothing else — thinking metadata is the profile's own word.
type piModel struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
}

// piDeclaredThinking reports whether a person declared that this model thinks.
// The provider's own /models list does not say, and a level invented from a
// model name would be a guess, so the declaration travels on the provider.
func piDeclaredThinking(cfg modelprovider.ModelProviderConfig, modelID string) bool {
	for _, id := range cfg.Reasoning {
		// A provider's id stands alone here; pi-namespaced, it may carry the
		// provider in front.
		if id == modelID || strings.HasSuffix(id, "/"+modelID) {
			return true
		}
	}
	return false
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
	// A models.json that names no providers (or a null one) leaves the map nil.
	if file.Providers == nil {
		file.Providers = map[string]piProviderConfig{}
	}
	if file.JazModels == nil {
		file.JazModels = map[string][]string{}
	}
	if file.JazThinking == nil {
		file.JazThinking = map[string][]string{}
	}
	declared := piModelsFile{Providers: map[string]piProviderConfig{}, JazModels: map[string][]string{}, JazThinking: map[string][]string{}}
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
		known := file.Providers[resolved.ID]
		entries := make([]map[string]any, 0, len(known.Models))
		for _, entry := range known.Models {
			entries = append(entries, entry)
		}
		declaredModels, thinking := piProviderEntries(entries, file.JazModels[resolved.ID], file.JazThinking[resolved.ID], resolved.Config, models)
		declared.Providers[resolved.ID] = piProviderConfig{
			Name:    firstNonEmpty(resolved.Config.Label, resolved.Meta.Label),
			BaseURL: baseURL,
			API:     piProviderAPI,
			APIKey:  apiKey,
			Headers: resolved.Config.Headers,
			Models:  declaredModels,
		}
		declared.JazThinking[resolved.ID] = thinking
		// A file with no index predates it and was written by Jaz alone, so
		// everything it lists is Jaz's.
		written, indexed := file.JazModels[resolved.ID]
		if !indexed {
			written = piModelIDsOf(entries)
		}
		declared.JazModels[resolved.ID] = jazModelIDs(entries, written, indexed, models)
	}
	for id, provider := range declared.Providers {
		file.Providers[id] = provider
		file.JazModels[id] = declared.JazModels[id]
		file.JazThinking[id] = declared.JazThinking[id]
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

// jazModelIDs is which of the provider's models Jaz owns, and so refreshes from
// the provider's list: the ones it wrote last time, plus the ones it is writing
// for the first time. An entry that was already in the file and is not Jaz's is
// a person's, and never becomes Jaz's — otherwise the next refresh would
// replace what they declared.
//
// A file with no index at all predates it, and only Jaz ever wrote that file,
// so everything in it is Jaz's and keeps being refreshed.
func jazModelIDs(known []map[string]any, written []string, indexed bool, models []piModel) []string {
	if !indexed {
		return piModelIDsOfKnown(known, models)
	}
	ours := make(map[string]struct{}, len(written))
	for _, id := range written {
		ours[id] = struct{}{}
	}
	before := make(map[string]struct{}, len(known))
	for _, entry := range known {
		before[piModelID(entry)] = struct{}{}
	}
	out := make([]string, 0, len(models))
	for _, model := range models {
		if _, wrote := ours[model.ID]; wrote {
			out = append(out, model.ID)
			continue
		}
		if _, existed := before[model.ID]; !existed {
			out = append(out, model.ID)
		}
	}
	return out
}

// piModelIDsOfKnown is, for a file written before the index existed, the
// provider's models as it listed them: each is Jaz's and stays current.
func piModelIDsOfKnown(known []map[string]any, models []piModel) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.ID)
	}
	return out
}

func piModelIDsOf(entries []map[string]any) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, piModelID(entry))
	}
	return out
}

// piProviderEntries is a provider's model list after this refresh. Jaz keeps
// current the entries it added itself and drops the models the provider
// retired, so its own list never goes stale. Every other entry is carried over
// as it is: pi reads an entry without reasoning as a model that cannot think,
// so replacing one would drop what it was told about that model.
//
// A provider's /models list says ids, names and context lengths, never whether
// a model thinks. A model a person declared as thinking carries that, and only
// that: pi fills in its own levels, which Jaz never states.
// piProviderEntries is a provider's model list after this refresh, plus which
// of the models Jaz called thinking: the ones a person declared on the provider.
//
// A model Jaz added is refreshed from the provider's own list, so a rename
// takes effect and a retired model leaves. Whether it thinks is not part of
// that list, so it is decided here from the declaration: one Jaz wrote itself
// stops applying when it is withdrawn, while a reasoning someone wrote into
// models.json by hand is theirs and stays.
//
// Every entry Jaz did not add is carried over as it is. pi reads an entry
// without reasoning as a model that cannot think, so replacing one would drop
// what it was told about that model.
func piProviderEntries(known []map[string]any, jazIDs, jazThinking []string, cfg modelprovider.ModelProviderConfig, models []piModel) ([]map[string]any, []string) {
	byID := make(map[string]map[string]any, len(known))
	for _, entry := range known {
		byID[piModelID(entry)] = entry
	}
	jaz := make(map[string]struct{}, len(jazIDs))
	for _, id := range jazIDs {
		jaz[id] = struct{}{}
	}
	thought := make(map[string]struct{}, len(jazThinking))
	for _, id := range jazThinking {
		thought[id] = struct{}{}
	}
	out := make([]map[string]any, 0, len(models)+len(known))
	thinking := make([]string, 0, len(models))
	listed := make(map[string]struct{}, len(models))
	for _, model := range models {
		listed[model.ID] = struct{}{}
		declared, ours := byID[model.ID], false
		if _, ok := jaz[model.ID]; ok {
			ours = true
		}
		thinks := piDeclaredThinking(cfg, model.ID)
		if thinks {
			thinking = append(thinking, model.ID)
		}
		entry := piModelEntry(model)
		if ours {
			// Jaz states a model's id, name and context window; everything else
			// the entry already held is still what someone declared about it.
			for key, value := range declared {
				if _, own := entry[key]; !own {
					entry[key] = value
				}
			}
		} else if declared != nil {
			entry = declared
		}
		// Whether it thinks is the provider's declaration and no entry's own
		// word: one Jaz wrote and no longer sees declared stops applying, while
		// a reasoning someone wrote into models.json by hand is theirs.
		if thinks {
			entry["reasoning"] = true
		} else if _, was := thought[model.ID]; ours && was {
			delete(entry, "reasoning")
		}
		out = append(out, entry)
	}
	for _, entry := range known {
		id := piModelID(entry)
		if _, ok := listed[id]; ok {
			continue
		}
		if _, ours := jaz[id]; ours {
			continue
		}
		out = append(out, entry)
	}
	return out, thinking
}

func piModelEntry(model piModel) map[string]any {
	entry := map[string]any{"id": model.ID}
	if model.Name != "" {
		entry["name"] = model.Name
	}
	if model.ContextWindow > 0 {
		entry["contextWindow"] = model.ContextWindow
	}
	return entry
}

func piModelID(entry map[string]any) string {
	id, _ := entry["id"].(string)
	return id
}

func piModelIDs(models []piModel) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.ID)
	}
	return out
}

// piProviderModels is the model list pi may run for a provider Jaz supplies:
// what the provider advertises plus its default and the thread's selection, in
// pi's provider-relative id. pi refuses a model its catalog does not list.
func piProviderModels(source piModelSource, defaultModel, selectedProvider, selected string) []piModel {
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
	return models
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

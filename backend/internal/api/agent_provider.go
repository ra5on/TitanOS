package api

import (
	"errors"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Outside providers for the KI-Agent (0.96): instead of an Ollama server a
// service with the OpenAI chat API, or Claude. Off from the factory. The
// key stays on the NAS like the tokens of the MCP connections: it never
// goes to the page, an empty one in a request keeps the stored one – but
// only for the same provider and address, so that changing the address
// cannot send a stored key somewhere else.

type agentProvider struct {
	// Kind: "" (Ollama), "openai" or "claude".
	Kind string `json:"kind,omitempty"`
	// URL of an OpenAI-compatible service, e.g. https://api.openai.com/v1.
	URL      string `json:"url,omitempty"`
	Key      string `json:"key,omitempty"`
	KeySet   bool   `json:"keySet,omitempty"`
	ClearKey bool   `json:"clearKey,omitempty"`
}

// view is the provider without its key.
func (p agentProvider) view() agentProvider {
	p.KeySet, p.Key, p.ClearKey = p.Key != "", "", false
	return p
}

// checkAgentProvider checks the provider of a request and fills in the
// stored key where the request left it out.
func checkAgentProvider(req *agentSettings, stored agentProvider) error {
	p := &req.Provider
	p.Kind, p.URL, p.Key = strings.TrimSpace(p.Kind), strings.TrimSpace(p.URL), strings.TrimSpace(p.Key)
	if !agent.ValidProvider(p.Kind) {
		return errors.New("unknown provider")
	}
	if len(p.Key) > 1000 {
		return errors.New("the key is too long")
	}
	switch p.Kind {
	case agent.ProviderOllama:
		// Nothing of an outside provider is kept, also no key.
		*p = agentProvider{}
		return nil
	case agent.ProviderOpenAI:
		u, err := agent.ParseProviderURL(p.URL)
		if err != nil {
			return errors.New("the provider's address: " + err.Error())
		}
		p.URL = u.String()
	case agent.ProviderClaude:
		p.URL = ""
	}
	switch {
	case p.ClearKey:
		p.Key = ""
	case p.Key == "" && stored.Kind == p.Kind && stored.URL == p.URL:
		p.Key = stored.Key
	}
	p.ClearKey, p.KeySet = false, false
	if p.Kind == agent.ProviderClaude && p.Key == "" && req.Enabled {
		return errors.New("the provider Claude needs an API key")
	}
	return nil
}

// client returns the client of an outside provider.
func (p agentProvider) client() (*agent.Ollama, error) {
	switch p.Kind {
	case agent.ProviderOpenAI:
		return agent.NewOpenAI(p.URL, p.Key)
	case agent.ProviderClaude:
		return agent.NewClaude(p.Key)
	}
	return nil, errors.New("no outside provider set")
}

package agent

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// CheckLocalWeights refuses Ollama's cloud aliases before private documents
// enter a request. Local aliases must identify the architecture of their weights.
func (o *Ollama) CheckLocalWeights(ctx context.Context, name string) error {
	if o.remote != nil || strings.Contains(strings.ToLower(name), "cloud") {
		return errors.New("model has no verified local weights")
	}
	var info struct {
		RemoteModel string `json:"remote_model"`
		RemoteHost  string `json:"remote_host"`
		Info        struct {
			Architecture string `json:"general.architecture"`
		} `json:"model_info"`
	}
	if err := o.do(ctx, http.MethodPost, "/api/show", map[string]string{"model": name}, &info); err != nil {
		return err
	}
	if info.RemoteModel != "" || info.RemoteHost != "" || info.Info.Architecture == "" {
		return errors.New("model has no verified local weights")
	}
	return nil
}

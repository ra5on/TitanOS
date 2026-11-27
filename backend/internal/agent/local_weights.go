package agent

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

var ErrNoLocalChat = errors.New("model has no verified local chat weights")

// CheckLocalChatModel refuses Ollama's cloud aliases before private documents
// enter a request. Local aliases must identify the architecture of their weights.
func (o *Ollama) CheckLocalChatModel(ctx context.Context, name string) error {
	if o.remote != nil || strings.Contains(strings.ToLower(name), "cloud") {
		return ErrNoLocalChat
	}
	var info struct {
		Capabilities []string `json:"capabilities"`
		RemoteModel  string   `json:"remote_model"`
		RemoteHost   string   `json:"remote_host"`
		Info         struct {
			Architecture string `json:"general.architecture"`
		} `json:"model_info"`
	}
	if err := o.do(ctx, http.MethodPost, "/api/show", map[string]string{"model": name}, &info); err != nil {
		return err
	}
	// Older local servers omit capabilities. If the model advertises them,
	// embedding-only or image-generation weights cannot answer a chat request.
	if info.RemoteModel != "" || info.RemoteHost != "" || info.Info.Architecture == "" || (info.Capabilities != nil && !contains(info.Capabilities, "completion")) {
		return ErrNoLocalChat
	}
	return nil
}

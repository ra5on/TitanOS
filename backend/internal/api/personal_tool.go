package api

import (
	"context"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

// Prepare is read-only and deterministic for side effects. Apply receives the exact, freshly
// rebuilt preview that was approved. It revalidates resource permissions on the native operation.
type personalTool struct {
	// EffectKey binds idempotency to a native object across different submissions.
	EffectKey      func(auth.User, pa.Request, pa.Preview) string
	SourceKinds    []string
	ID, Title, App string
	Sensitive      bool
	Default        pa.Level // shipped ceiling, otherwise never; personal default ask or never
	Available      func(auth.User) error
	Check          func(context.Context, auth.User, pa.Request) error
	Prepare        func(context.Context, auth.User, pa.Request) (pa.Preview, error)
	Apply          func(context.Context, auth.User, pa.Request, pa.Preview) (pa.Result, error)
}

// Registered by package init only. Each factory binds native handlers of one Server.
var personalToolFactories []func(*Server) []personalTool

func registerPersonalTools(f func(*Server) []personalTool) {
	personalToolFactories = append(personalToolFactories, f)
}

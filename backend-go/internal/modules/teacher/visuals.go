package teacher

import (
	"context"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/visuals"
)

// VisualImage exposes only the bounded provider-resource adapter to HTTP.
func VisualImage(ctx context.Context, provider, asset string) ([]byte, string, error) {
	return visuals.Image(ctx, provider, asset)
}

func VisualMetadata(ctx context.Context, provider, asset string) (visuals.Figure, error) {
	return visuals.Metadata(ctx, provider, asset)
}

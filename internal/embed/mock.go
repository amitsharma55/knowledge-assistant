// Package embed holds embedding implementations shared between services.
// The MockEmbedder is used in local dev and tests so we can exercise the
// pipeline without spending Bedrock tokens.
package embed

import "context"

type Mock struct{ Dim int }

func (m Mock) Embed(_ context.Context, text string) ([]float32, error) {
	dim := m.Dim
	if dim == 0 {
		dim = 1024
	}
	v := make([]float32, dim)
	var h uint32 = 2166136261
	for i := 0; i < len(text); i++ {
		h ^= uint32(text[i])
		h *= 16777619
	}
	for i := range v {
		h = h*1103515245 + 12345
		v[i] = float32(int32(h)) / float32(1<<31)
	}
	return v, nil
}

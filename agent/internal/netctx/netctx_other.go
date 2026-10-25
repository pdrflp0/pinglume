//go:build !darwin

package netctx

import "context"

// System ainda não tem adaptador fora do macOS: a condição fica "desconhecida"
// e quem usa precisa declará-la.
type System struct{}

func (*System) Detect(context.Context) Context {
	return Context{Kind: KindUnknown, Method: "detecção não suportada nesta plataforma"}
}

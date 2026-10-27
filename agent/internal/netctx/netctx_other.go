//go:build !darwin

package netctx

import "context"

type System struct{}

func (*System) Detect(context.Context) Context {
	return Context{Kind: KindUnknown, Method: "detecção não suportada nesta plataforma"}
}

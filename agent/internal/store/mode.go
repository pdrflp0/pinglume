package store

import (
	"context"
	"errors"
)

const (
	ModeReal = "real"
	ModeDemo = "demo"
)

var (
	ErrDemoData = errors.New("este diretório contém dados de demonstração; use `pinglume demo` ou outro --data-dir")
	ErrRealData = errors.New("este diretório contém medições reais; o modo demonstração usa um banco separado (--data-dir)")
)

func (s *Store) Mode(ctx context.Context) (string, error) {
	v, ok, err := s.GetState(ctx, "mode")
	if err != nil || !ok {
		return ModeReal, err
	}
	return v, nil
}

func (s *Store) hasData(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM observations) + (SELECT COUNT(*) FROM observation_minutes) + (SELECT COUNT(*) FROM incidents)`).Scan(&n)
	return n > 0, err
}

func (s *Store) ClaimMode(ctx context.Context, want string) error {
	current, err := s.Mode(ctx)
	if err != nil {
		return err
	}
	_, marked, err := s.GetState(ctx, "mode")
	if err != nil {
		return err
	}
	switch {
	case want == ModeReal && current == ModeDemo:
		return ErrDemoData
	case want == ModeDemo && current != ModeDemo:
		if has, err := s.hasData(ctx); err != nil {
			return err
		} else if has {
			return ErrRealData
		}
	case marked:
		return nil
	}
	return s.SetState(ctx, "mode", want)
}

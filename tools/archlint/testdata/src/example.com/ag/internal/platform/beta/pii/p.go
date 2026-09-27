package pii

import (
	"fmt"
	"log/slog"
)

type Household struct {
	Ref   string
	Name  string `pii:"true"`
	Phone string `pii:"true"`
}

type Wrapper struct {
	H Household
}

func Log(h Household, w Wrapper) error {
	slog.Info("hộ", "ref", h.Ref)
	slog.Info("hộ", "name", h.Name)        // want "R-14"
	slog.Warn("hộ", "h", h)                // want "R-14"
	_ = fmt.Sprintf("%v", w)               // want "R-14"
	return fmt.Errorf("sai số %s", h.Phone) // want "R-14"
}

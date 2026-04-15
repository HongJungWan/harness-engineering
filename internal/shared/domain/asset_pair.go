package domain

import (
	"fmt"
	"strings"
)

type AssetPair struct {
	Base  string
	Quote string
}

func NewAssetPair(base, quote string) (AssetPair, error) {
	if base == "" || quote == "" {
		return AssetPair{}, fmt.Errorf("base and quote must not be empty")
	}
	if base == quote {
		return AssetPair{}, fmt.Errorf("base and quote must be different")
	}
	return AssetPair{Base: base, Quote: quote}, nil
}

func NewAssetPairFromString(s string) (AssetPair, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return AssetPair{}, fmt.Errorf("invalid asset pair format: %q (expected BASE/QUOTE)", s)
	}
	return NewAssetPair(parts[0], parts[1])
}

func (ap AssetPair) String() string {
	return fmt.Sprintf("%s/%s", ap.Base, ap.Quote)
}

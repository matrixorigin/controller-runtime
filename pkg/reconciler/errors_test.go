package reconciler

import (
	"github.com/go-errors/errors"
	"testing"
)

func TestIsNil(t *testing.T) {
	err := wraperr()
	if !IsNil(err) {
		t.Fatal("nil error with interface should be nil")
	}
}

func wraperr() error {
	return errors.Wrap(nilerr(), 0)
}

func nilerr() error {
	return nil
}

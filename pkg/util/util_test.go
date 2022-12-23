package util

import (
	"fmt"
	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"testing"
)

func TestIgnore(t *testing.T) {
	tests := []struct {
		name    string
		isErr   func(error) bool
		err     error
		wantErr bool
	}{{
		name:    "ignore nil",
		isErr:   apierrors.IsAlreadyExists,
		err:     nil,
		wantErr: false,
	}, {
		name:    "ignore matched error",
		isErr:   apierrors.IsAlreadyExists,
		err:     apierrors.NewAlreadyExists(schema.GroupResource{Group: "test", Resource: "test"}, "test"),
		wantErr: false,
	}, {
		name:    "return unmatched error",
		isErr:   apierrors.IsAlreadyExists,
		err:     fmt.Errorf("test"),
		wantErr: true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Ignore(tt.isErr, tt.err); (err != nil) != tt.wantErr {
				t.Errorf("Ignore() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUpsertListByKey(t *testing.T) {
	type elem struct {
		Key   string
		Value string
	}
	keyFunc := func(v elem) string {
		return v.Key
	}
	tests := []struct {
		name       string
		targetList []elem
		elemList   []elem
		want       []elem
	}{{
		name:       "upsert empty",
		targetList: []elem{{Key: "a", Value: "apple"}},
		elemList:   nil,
		want:       []elem{{Key: "a", Value: "apple"}},
	}, {
		name:       "upsert list",
		targetList: []elem{{Key: "a", Value: "apple"}, {Key: "b", Value: "banana"}},
		elemList:   []elem{{Key: "a", Value: "alice"}, {Key: "c", Value: "cat"}, {Key: "d", Value: "dog"}},
		want:       []elem{{Key: "a", Value: "alice"}, {Key: "b", Value: "banana"}, {Key: "c", Value: "cat"}, {Key: "d", Value: "dog"}},
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(UpsertListByKey(tt.targetList, tt.elemList, keyFunc), tt.want); diff != "" {
				t.Errorf("UpsertListByKey(), diff:\n%s", diff)
			}
		})
	}
}

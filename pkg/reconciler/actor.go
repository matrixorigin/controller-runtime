// Copyright 2022 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package reconciler

import (
	"context"
	"fmt"
	"reflect"
	"runtime"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Actor[T client.Object] interface {
	Observe(*Context[T]) (Action[T], error)
	Finalize(*Context[T]) (done bool, err error)
}

type Action[T client.Object] func(*Context[T]) error

func (s Action[T]) String() string {
	return runtime.FuncForPC(reflect.ValueOf(s).Pointer()).Name()
}

type KubeClient interface {
	Create(obj client.Object, opts ...client.CreateOption) error
	CreateOwned(obj client.Object, opts ...client.CreateOption) error
	Get(objKey client.ObjectKey, obj client.Object) error
	Update(obj client.Object, opts ...client.UpdateOption) error
	UpdateStatus(obj client.Object, opts ...client.SubResourceUpdateOption) error
	Delete(obj client.Object, opts ...client.DeleteOption) error
	List(objList client.ObjectList, opts ...client.ListOption) error
	Patch(obj client.Object, mutateFn func() error, opts ...client.PatchOption) error
	Exist(objKey client.ObjectKey, kind client.Object) (bool, error)
}

var _ KubeClient = &Context[client.Object]{}

type Context[T client.Object] struct {
	context.Context
	Obj T

	// Dep hold the dependencies of the object T, will only be set when
	// the object implement the `Dependant` interface
	Dep T

	Client client.Client
	// TODO(aylei): add tracing
	Event EventEmitter
	Log   logr.Logger
}

// TODO(aylei): add logging and tracing when operate upon kube-api
func (c *Context[T]) Create(obj client.Object, opts ...client.CreateOption) error {
	return c.Client.Create(c, obj, opts...)
}

func (c *Context[T]) Get(objKey client.ObjectKey, obj client.Object) error {
	return c.Client.Get(c, objKey, obj)
}

// Update the spec of the given obj
func (c *Context[T]) Update(obj client.Object, opts ...client.UpdateOption) error {
	return c.Client.Update(c, obj, opts...)
}

// UpdateStatus update the status of the given obj
func (c *Context[T]) UpdateStatus(obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return c.Client.Status().Update(c, obj, opts...)
}

// Delete marks the given obj to be deleted
func (c *Context[T]) Delete(obj client.Object, opts ...client.DeleteOption) error {
	return c.Client.Delete(c, obj, opts...)
}

func (c *Context[T]) List(objList client.ObjectList, opts ...client.ListOption) error {
	return c.Client.List(c, objList, opts...)
}

// Patch patches the mutation by mutateFn to the spec of given obj
// an error would be raised if mutateFn changed anything immutable (e.g. namespace / name).
// Changes will be merged with current object, optimisticLock is enforced.
func (c *Context[T]) Patch(obj client.Object, mutateFn func() error, opts ...client.PatchOption) error {
	patch, err := c.buildPatch(obj, mutateFn)
	if patch == nil {
		return err
	}
	return c.Client.Patch(c, obj, *patch, opts...)
}

// PatchStatus patches the mutation by mutateFn to the status of given obj
// an error would be raised if mutateFn changed anything immutable (e.g. namespace / name)
func (c *Context[T]) PatchStatus(obj client.Object, mutateFn func() error, opts ...client.SubResourcePatchOption) error {
	patch, err := c.buildPatch(obj, mutateFn)
	if patch == nil {
		return err
	}
	return c.Client.Status().Patch(c, obj, *patch, opts...)
}

func (c *Context[T]) buildPatch(obj client.Object, mutateFn func() error) (*client.Patch, error) {
	key := client.ObjectKeyFromObject(obj)
	before := obj.DeepCopyObject().(client.Object)
	if err := mutateFn(); err != nil {
		return nil, err
	}
	if newKey := client.ObjectKeyFromObject(obj); key != newKey {
		return nil, fmt.Errorf("MutateFn cannot mutate object name and/or object namespace")
	}
	if reflect.DeepEqual(before, obj) {
		// no change to patch
		return nil, nil
	}
	p := client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})
	return &p, nil
}

// CreateOwned create the given object with an OwnerReference to the currently reconciling
// controller object (ctx.Obj)
func (c *Context[T]) CreateOwned(obj client.Object, opts ...client.CreateOption) error {
	if err := controllerutil.SetControllerReference(c.Obj, obj, c.Client.Scheme()); err != nil {
		return err
	}
	return c.Client.Create(c, obj, opts...)
}

func (c *Context[T]) Exist(objKey client.ObjectKey, kind client.Object) (bool, error) {
	err := c.Get(objKey, kind)
	if err != nil && apierrors.IsNotFound(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

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
package webhook

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"strings"
)

type Handler[T runtime.Object] interface {
	Default(obj T)
	ValidateCreate(obj T) (warnings admission.Warnings, err error)
	ValidateUpdate(oldObj, newObj T) (warnings admission.Warnings, err error)
	ValidateDelete(obj T) (warnings admission.Warnings, err error)
	GetObject() T
}

// RegisterWebhook regist a webhook.Handler to the webhook server
func RegisterWebhook[T runtime.Object](server ctrlwebhook.Server, resourcePath string, handler Handler[T], s *runtime.Scheme) {
	path := strings.TrimSuffix(resourcePath, "/")
	w := &wrapper[T]{handler: handler}
	dw := admission.WithCustomDefaulter(s, handler.GetObject(), w)
	server.Register(path+"/defaulting", dw)
	vw := admission.WithCustomValidator(s, handler.GetObject(), w)
	server.Register(path+"/validating", vw)
}

type wrapper[T runtime.Object] struct {
	handler Handler[T]
}

func (w *wrapper[T]) Default(_ context.Context, obj runtime.Object) error {
	w.handler.Default(obj.(T))
	return nil
}

func (w *wrapper[T]) ValidateCreate(_ context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	return w.handler.ValidateCreate(obj.(T))
}

func (w *wrapper[T]) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error) {
	return w.handler.ValidateUpdate(oldObj.(T), newObj.(T))
}

func (w *wrapper[T]) ValidateDelete(_ context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	return w.handler.ValidateDelete(obj.(T))
}

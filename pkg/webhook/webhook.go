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
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type Handler[T runtime.Object] interface {
	Default(obj T)
	ValidateCreate(obj T) (warnings admission.Warnings, err error)
	ValidateUpdate(oldObj, newObj T) (warnings admission.Warnings, err error)
	ValidateDelete(obj T) (warnings admission.Warnings, err error)
	GetObject() T
}

// ExtendedHandler is a handler that can mutate on create and update
// it works after the defaulting
type ExtendedHandler[T runtime.Object] interface {
	MutateOnCreate(ctx context.Context, obj T) (err error)
	MutateOnUpdate(ctx context.Context, oldObj, newObj T) (err error)
}

// RegisterWebhook regist a webhook.Handler to the webhook server
func RegisterWebhook[T runtime.Object](server ctrlwebhook.Server, resourcePath string, handler Handler[T], s *runtime.Scheme) {
	path := strings.TrimSuffix(resourcePath, "/")
	w := &wrapper[T]{handler: handler, decoder: admission.NewDecoder(s)}

	// Check if the handler also implements ExtendedHandler
	if extHandler, ok := handler.(ExtendedHandler[T]); ok {
		w.extendedHandler = extHandler
	}

	dw := admission.WithCustomDefaulter(s, handler.GetObject(), w)
	server.Register(path+"/defaulting", dw)
	vw := admission.WithCustomValidator(s, handler.GetObject(), w)
	server.Register(path+"/validating", vw)
}

type wrapper[T runtime.Object] struct {
	decoder         *admission.Decoder
	handler         Handler[T]
	extendedHandler ExtendedHandler[T]
}

func (w *wrapper[T]) Default(ctx context.Context, obj runtime.Object) error {
	w.handler.Default(obj.(T))
	if w.extendedHandler != nil {
		req, err := admission.RequestFromContext(ctx)
		if err != nil {
			return err
		}
		switch req.AdmissionRequest.Operation {
		case admissionv1.Create:
			return w.extendedHandler.MutateOnCreate(ctx, obj.(T))
		case admissionv1.Update:
			oldObj := w.handler.GetObject()
			if decodeErr := w.decoder.DecodeRaw(req.AdmissionRequest.OldObject, oldObj); decodeErr != nil {
				return decodeErr
			}
			return w.extendedHandler.MutateOnUpdate(ctx, oldObj, obj.(T))
		}
	}
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

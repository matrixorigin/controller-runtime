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
	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	recon "sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ recon.Reconciler = &Reconciler[client.Object]{}

func TestReconciler(t *testing.T) {
	cases := map[string]struct {
		mgr    manager.Manager
		actor  Actor[*corev1.Pod]
		result recon.Result
		err    error
	}{}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			r, err := newReconciler(&corev1.Pod{}, "test", tc.mgr, tc.actor, &options{})
			g.Expect(err).To(Succeed())
			got, err := r.Reconcile(context.Background(), recon.Request{})
			if diff := cmp.Diff(tc.err, err); diff != "" {
				t.Errorf("\n%s\nr.Reconcile(...): -want error, +got error:\n%s", name, diff)
			}
			if diff := cmp.Diff(tc.result, got); diff != "" {
				t.Errorf("\n%s\nr.Reconcile(...): -want, +got:\n%s", name, diff)
			}
		})
	}
}

func TestSetupObjectFactory(t *testing.T) {
	g := NewGomegaWithT(t)
	r := &Reconciler[*corev1.Service]{}
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	g.Expect(r.setupObjectFactory(s, &corev1.Service{})).To(Succeed())
	g.Expect(r.newT()).ToNot(BeNil())
}

type FakeActor struct {
	ObserveFn  func(*Context[*corev1.Pod]) (Action[*corev1.Pod], error)
	FinalizeFn func(*Context[*corev1.Pod]) (done bool, err error)
}

func (r *FakeActor) Observe(ctx *Context[*corev1.Pod]) (Action[*corev1.Pod], error) {
	if r.ObserveFn != nil {
		return r.ObserveFn(ctx)
	}
	return nil, nil
}

func (r *FakeActor) Finalize(ctx *Context[*corev1.Pod]) (done bool, err error) {
	if r.FinalizeFn != nil {
		return r.FinalizeFn(ctx)
	}
	return true, nil
}

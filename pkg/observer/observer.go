package observer

import (
	recon "github.com/matrixorigin/controller-runtime/pkg/reconciler"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Setup[T client.Object](tpl T, name string, mgr ctrl.Manager, observer Observer[T], applyOpts ...recon.ApplyOption) error {
	return recon.Setup(tpl, name, mgr, asActor(observer), applyOpts...)
}

type Observer[T client.Object] interface {
	Observe(*recon.Context[T]) error
}

func asActor[T client.Object](o Observer[T]) recon.Actor[T] {
	return &observerActor[T]{observer: o}
}

type observerActor[T client.Object] struct {
	observer Observer[T]
}

func (o *observerActor[T]) Observe(ctx *recon.Context[T]) (recon.Action[T], error) {
	return nil, o.observer.Observe(ctx)
}

func (o *observerActor[T]) Finalize(_ *recon.Context[T]) (done bool, err error) {
	return true, nil
}

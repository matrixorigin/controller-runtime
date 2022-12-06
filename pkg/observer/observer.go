package observer

import (
	recon "github.com/matrixorigin/controller-runtime/pkg/reconciler"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Observer[T client.Object] interface {
	Observe(*recon.Context[T]) error
}

func AsActor[T client.Object](o Observer[T]) recon.Actor[T] {
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

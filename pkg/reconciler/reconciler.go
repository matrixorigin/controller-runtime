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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	recon "sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-errors/errors"
	"github.com/go-logr/logr"
	"github.com/matrixorigin/controller-runtime/pkg/util"
)

const (
	finalizerPrefix  = "matrixorigin.io"
	finalizeFail     = "FinalizeFail"
	reconcileFail    = "ReconcileFail"
	reconcileSuccess = "ReconcileSuccess"
)

const (
	// Following analog to https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md

	Error        = 0
	Warn         = 1
	Info         = 2
	ExtendedInfo = 3
	Debug        = 4
	Trace        = 5

	// We don't want to retry immediately since if the interval between two reconcile loops is smaller than the
	// list-and-watch cache lag, there is wasted operations. This is does not affect correctness but do introduce
	// unnecessary load on kube-apiserver.
	defaultRequeueAfter = 2 * time.Second
)

var (
	// retry means retry after certain period
	retry  = recon.Result{Requeue: true, RequeueAfter: defaultRequeueAfter}
	forget = recon.Result{Requeue: false}
	// backoff means exponential backoff
	backoff = recon.Result{Requeue: true}
)

type Reconciler[T client.Object] struct {
	*options
	client.Client

	name  string
	actor Actor[T]
	newT  func() T
}

type options struct {
	recorder record.EventRecorder
	logger   logr.Logger
	buildFn  func(*builder.Builder)
	ctrlOpts controller.Options
	// skipFinalizer indicates the reconciler can skip processing finalizer
	skipFinalizer bool
	// skipPatchFinalizer indicates reconciler will handle CR deletion but do not patch finalizer
	skipPatchFinalizer bool
	// skipStatusSync indicates the reconciler can skip sync status
	skipStatusSync bool

	pred *predicate.Predicate
}

type ApplyOption func(*options)

// WithEventRecorder set the event recorder of the reconciler
func WithEventRecorder(recorder record.EventRecorder) ApplyOption {
	return func(o *options) { o.recorder = recorder }
}

// WithControllerOptions set the controller options of the reconciler
func WithControllerOptions(opts controller.Options) ApplyOption {
	return func(o *options) { o.ctrlOpts = opts }
}

// WithBuildFn allows customizing reconciler.Builder defined the controller-runtime
func WithBuildFn(buildFn func(*builder.Builder)) ApplyOption {
	return func(o *options) { o.buildFn = buildFn }
}

func WithPredicate(pred predicate.Predicate) ApplyOption {
	return func(o *options) { o.pred = &pred }
}

func SkipFinalizer() ApplyOption {
	return func(o *options) { o.skipFinalizer = true }
}

func SkipPatchFinalizer() ApplyOption {
	return func(o *options) { o.skipPatchFinalizer = true }
}

func SkipStatusSync() ApplyOption {
	return func(o *options) { o.skipStatusSync = true }
}

// Setup register a kubernetes reconciler to the resource kind defined by T.
// Name is the name of the reconciler, which should be unique across a cluster.
// Manager represents the kubernetes cluster.
// Actor implements the logic of the reconciliation.
func Setup[T client.Object](tpl T, name string, mgr ctrl.Manager, actor Actor[T], applyOpts ...ApplyOption) error {
	opts := &options{
		recorder: mgr.GetEventRecorderFor(name),
		logger:   mgr.GetLogger().WithValues("controller", name),
	}
	for _, applyOpt := range applyOpts {
		applyOpt(opts)
	}
	r, err := newReconciler(tpl, name, mgr, actor, opts)
	if err != nil {
		return err
	}

	// register reconciler to the target kubernetes cluster
	// TODO(aylei): figure out what sub-resources should be owned here
	obj := r.newT()
	bld := ctrl.NewControllerManagedBy(mgr)
	if opts.buildFn != nil {
		opts.buildFn(bld)
	}
	// ignore status change
	var filter predicate.Predicate
	if opts.pred != nil {
		filter = *opts.pred
	} else {
		// default preds
		filter = predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)
	}

	return bld.Named(r.name).
		WithOptions(r.ctrlOpts).
		For(obj, builder.WithPredicates(filter)).
		Complete(r)
}

func newReconciler[T client.Object](tpl T, name string, mgr ctrl.Manager, actor Actor[T], opts *options) (*Reconciler[T], error) {
	r := &Reconciler[T]{
		options: opts,
		Client:  mgr.GetClient(),

		name:  name,
		actor: actor,
	}

	// resolve go type to GVK and build the factory of T
	if err := r.setupObjectFactory(mgr.GetScheme(), tpl); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Reconciler[T]) Reconcile(goCtx context.Context, req recon.Request) (recon.Result, error) {
	log := r.logger.WithValues("namespace", req.Namespace, "name", req.Name)
	log.V(Debug).Info("start reconciling")

	// get the latest spec and status from apiserver and build the action context
	obj := r.newT()
	if err := r.Get(goCtx, req.NamespacedName, obj); err != nil {
		// forget the object if it does not exist
		return forget, util.Ignore(kerr.IsNotFound, err)
	}
	ctx := &Context[T]{
		Context: goCtx,
		Obj:     obj,
		Client:  r.Client,
		Log:     log,
		Event:   &EmitEventWrapper{EventRecorder: r.recorder, subject: obj},
	}

	// optionally transit to deleting state
	if util.WasDeleted(obj) {
		return r.finalize(ctx)
	}

	if _, ok := any(obj).(Dependant); ok {
		depHolder := obj.DeepCopyObject().(Dependant)
		ready, err := r.waitDependencies(ctx, depHolder)
		if err != nil {
			return backoff, errors.WrapPrefix(err, "error waiting dependencies to be ready", 0)
		}
		if !ready {
			ctx.Log.Info("dependency not ready, retry")
			return retry, nil
		}
		ctx.Dep = depHolder.(T)
	}

	// ensure finalizer before any action to guarantee completeness of finalizing
	if err := r.ensureFinalizer(ctx, obj); err != nil {
		if kerr.IsConflict(err) {
			ctx.Log.V(Debug).Info("add finalizer conflict error", "detail", err.Error())
			return retry, nil
		}
		return backoff, errors.Wrap(err, 0)
	}

	action, err := r.actor.Observe(ctx)
	if err != nil {
		return r.processActorError(ctx, err)
	}

	cond, isConditional := any(obj).(Conditional)

	// No action to take implies the object reached desired state, we forget it
	// now and wait for the next change to be watched or some resync timeouts.
	if action == nil {
		ctx.Log.Info("object is synced, reconcile will be triggered on next change or resync")
		ctx.Event.EmitEventGeneric(reconcileSuccess, "object is synced", nil)

		if isConditional {
			cond.SetCondition(synced(true, obj.GetGeneration()))
		}
		if err := r.updateStatus(ctx); err != nil {
			if kerr.IsConflict(err) {
				log.V(Debug).Info("update status conflict, retry", "detail", err.Error())
				return retry, nil
			}
			return backoff, errors.Wrap(err, 0)
		}
		return forget, nil
	}

	if isConditional {
		cond.SetCondition(synced(false, obj.GetGeneration()))
	}
	if err := r.updateStatus(ctx); err != nil {
		if kerr.IsConflict(err) {
			log.V(Debug).Info("update status conflict, retry", "detail", err.Error())
			return retry, nil
		}
		return backoff, errors.Wrap(err, 0)
	}

	log.V(Debug).Info("execute reconcile action", "action", action)
	if err := action(ctx); err != nil {
		return r.processActorError(ctx, err)
	}
	// Always retry after a successful action to check what should be done next
	return retry, nil
}

func (r *Reconciler[T]) updateStatus(ctx *Context[T]) error {
	if r.skipStatusSync {
		return nil
	}
	return ctx.UpdateStatus(ctx.Obj)
}

func (r *Reconciler[T]) processActorError(ctx *Context[T], actorErr error) (recon.Result, error) {
	if IsNil(actorErr) {
		ctx.Log.Error(actorErr, "nil error with interface is returned from reconciler")
		return backoff, nil
	}
	// 1. record error details
	obj := ctx.Obj
	if cond, isConditional := any(obj).(Conditional); isConditional {
		cond.SetCondition(metav1.Condition{
			Type:               ConditionTypeSynced,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: obj.GetGeneration(),
			Message:            fmt.Sprintf("Last error: %s", actorErr.Error()),
		})
	}
	if err := r.updateStatus(ctx); err != nil {
		if kerr.IsConflict(err) {
			ctx.Log.V(Debug).Info("update status conflict, retry", "detail", err.Error())
			return retry, nil
		}
		return retry, nil
	}

	// 2. check whether resync is requested
	var resync *ReSync
	if errors.As(actorErr, &resync) {
		// resync error
		ctx.Log.V(Debug).Info("actor request resync", "detail", resync.Error())
		return recon.Result{Requeue: true, RequeueAfter: resync.RequeueAfter}, nil
	}

	// 3. for conflict error, just log and retry
	if kerr.IsConflict(actorErr) {
		ctx.Log.V(Debug).Info("update conflict in reconcile, retry", "detail", actorErr.Error())
		return retry, nil
	}

	ctx.Event.EmitEventGeneric(reconcileFail, "failed calling actions", actorErr)
	// 4. print error stack if using error package "github.com/go-errors/errors"
	var stackErr *errors.Error
	if errors.As(actorErr, &stackErr) {
		ctx.Log.Error(actorErr, "error reconciling", "stack", stackErr.ErrorStack())
		return backoff, nil
	}
	return backoff, actorErr
}

func (r *Reconciler[T]) waitDependencies(ctx *Context[T], dt Dependant) (bool, error) {
	deps := dt.GetDependencies()
	for _, dep := range deps {
		ready, err := dep.IsReady(ctx)
		if err != nil {
			return false, err
		}
		if !ready {
			return false, nil
		}
	}
	return true, nil
}

func (r *Reconciler[T]) finalize(ctx *Context[T]) (recon.Result, error) {
	if !r.skipPatchFinalizer && !r.hasFinalizer(ctx.Obj) {
		// Finalizer work of current reconciler is done or not needed, the object might
		// wait other reconcilers to complete there finalizer work, ignore.
		return forget, nil
	}
	done, err := r.actor.Finalize(ctx)
	if err != nil {
		if IsNil(err) {
			ctx.Log.Error(err, "nil error with interface is returned from reconciler")
			return backoff, nil
		}
		// print error stack if using error package "github.com/go-errors/errors"
		var stackErr *errors.Error
		ctx.Event.EmitEventGeneric(finalizeFail, "failed to finalize object", err)
		if errors.As(err, &stackErr) {
			ctx.Log.Error(err, stackErr.ErrorStack())
			return backoff, nil
		}
		return backoff, err
	}
	if !done {
		ctx.Log.Info("does not complete finalizing, retry")
		return retry, nil
	}
	ctx.Log.Info("resource finalizing complete, remove finalizer")
	if err := r.removeFinalizer(ctx, ctx.Obj); err != nil {
		ctx.Event.EmitEventGeneric(finalizeFail, "failed to remove finalizer", err)
		return retry, nil
	}
	// object finalized and there is no more work for current reconciler, forget it
	return forget, nil
}

func (r *Reconciler[T]) setupObjectFactory(scheme *runtime.Scheme, tpl T) error {
	// type T must be registered in the scheme with only one certain GVK
	gvks, _, err := scheme.ObjectKinds(tpl)
	if err != nil {
		return err
	}
	if len(gvks) != 1 {
		return fmt.Errorf("expected 1 object kind for %T, got %d", tpl, len(gvks))
	}
	gvk := gvks[0]
	// check whether newT() can succeed and return error early to avoid panic
	_, err = scheme.New(gvk)
	if err != nil {
		return err
	}
	r.newT = func() T {
		v, err := scheme.New(gvk)
		// newT() must not return error with guard check above, so panic here
		if err != nil {
			panic(err)
		}
		return v.(T)
	}
	return nil
}

func (r *Reconciler[T]) trySetCondition(obj client.Object, c metav1.Condition) {
	if cond, ok := obj.(Conditional); ok {
		cond.SetCondition(c)
	}
	return
}

func (r *Reconciler[T]) finalizer() string {
	return fmt.Sprintf("%s/%s", finalizerPrefix, r.name)
}

func (c *Reconciler[T]) hasFinalizer(obj T) bool {
	return slices.Contains(obj.GetFinalizers(), c.finalizer())
}

func (c *Reconciler[T]) removeFinalizer(ctx *Context[T], obj T) error {
	if c.skipPatchFinalizer {
		return nil
	}
	if controllerutil.RemoveFinalizer(obj, c.finalizer()) {
		return ctx.Update(obj)
	}
	return nil
}

func (c *Reconciler[T]) ensureFinalizer(ctx *Context[T], obj T) error {
	if c.skipFinalizer {
		return nil
	}
	if c.skipPatchFinalizer {
		return nil
	}
	if controllerutil.AddFinalizer(obj, c.finalizer()) {
		return ctx.Update(obj)
	}
	return nil
}

func synced(b bool, generation int64) metav1.Condition {
	if b {
		return metav1.Condition{
			Type:               ConditionTypeSynced,
			ObservedGeneration: generation,
			Status:             metav1.ConditionTrue,
			Message:            "the object is synced",
		}
	}
	return metav1.Condition{
		Type:               ConditionTypeSynced,
		ObservedGeneration: generation,
		Status:             metav1.ConditionFalse,
		Message:            "the object is reconciling",
	}
}

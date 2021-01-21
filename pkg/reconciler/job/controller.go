/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package exception

import (
	"context"

	"knative.dev/pkg/tracker"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"

	pipelineclient "github.com/tektoncd/pipeline/pkg/client/injection/client"

	exceptionclient "github.com/vincentpli/exception-handler/pkg/client/injection/client"
	exceptioninformer "github.com/vincentpli/exception-handler/pkg/client/injection/informers/exception/v1alpha1/exception"
	runreconciler "github.com/tektoncd/pipeline/pkg/client/injection/reconciler/pipeline/v1alpha1/run"
	runinformer "github.com/tektoncd/pipeline/pkg/client/injection/informers/pipeline/v1alpha1/run"
	pipelineruninformer "github.com/tektoncd/pipeline/pkg/client/injection/informers/pipeline/v1beta1/pipelinerun"
	pipelinecontroller "github.com/tektoncd/pipeline/pkg/controller"
	exceptionv1alpha1 "github.com/vincentpli/exception-handler/pkg/apis/exception/v1alpha1"
	"k8s.io/client-go/tools/cache"
)

// NewController creates a Reconciler and returns the result of NewImpl.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)

	pipelineclientset := pipelineclient.Get(ctx)
	exceptionclientset := exceptionclient.Get(ctx)

	runInformer := runinformer.Get(ctx)
	exceptioninformer := exceptioninformer.Get(ctx)
	pipelineruninformer := pipelineruninformer.Get(ctx)

	r := &Reconciler{
		pipelineClientSet:  pipelineclientset,
		exceptionClientSet: exceptionclientset,
		runLister:          runInformer.Lister(),
		exceptionLister:    exceptioninformer.Lister(),
		pipelineRunLister:  pipelineruninformer.Lister(),
	}

	impl := runreconciler.NewImpl(ctx, r)
	r.Tracker = tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))

	logger.Info("Setting up event handlers.")

	exceptioninformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	runInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: pipelinecontroller.FilterRunRef(exceptionv1alpha1.SchemeGroupVersion.String(), "Exception"),
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	// Add event handler for Pipelineruns controlled by Run
	pipelineruninformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: pipelinecontroller.FilterOwnerRunRef(runInformer.Lister(), exceptionv1alpha1.SchemeGroupVersion.String(), "Exception"),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	return impl
}

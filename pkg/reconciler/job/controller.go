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

	// jobclient "github.com/vincentpli/concurrent-reconcile/pkg/client/injection/client"
	// jobinformer "github.com/vincentpli/concurrent-reconcile/pkg/client/injection/informers/job/v1alpha1/job"
	runreconciler "github.com/tektoncd/pipeline/pkg/client/injection/reconciler/pipeline/v1alpha1/run"
	runinformer "github.com/tektoncd/pipeline/pkg/client/injection/informers/pipeline/v1alpha1/run"
	jobv1alpha1 "github.com/vincentpli/concurrent-reconcile/pkg/apis/job/v1alpha1"
	"k8s.io/client-go/tools/cache"
)

// NewController creates a Reconciler and returns the result of NewImpl.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)

	pipelineclientset := pipelineclient.Get(ctx)
	jobclientset := jobclient.Get(ctx)

	runInformer := runinformer.Get(ctx)
	jobinformer := jobinformer.Get(ctx)
	pipelineruninformer := pipelineruninformer.Get(ctx)

	r := &Reconciler{
		pipelineClientSet:  pipelineclientset,
		// jobClientSet: jobclientset,
		runLister:          runInformer.Lister(),
		// jobLister:    jobinformer.Lister(),
	}

	impl := runreconciler.NewImpl(ctx, r)
	r.Tracker = tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))

	logger.Info("Setting up event handlers.")

	jobinformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	runInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: pipelinecontroller.FilterRunRef(exceptionv1alpha1.SchemeGroupVersion.String(), "Job"),
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	return impl
}

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
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	// v1alpha1 "github.com/vincentpli/exception-handler/pkg/apis/exception/v1alpha1"
	// exceptionreconciler "github.com/vincentpli/exception-handler/pkg/client/injection/reconciler/exception/v1alpha1/exception"
	"github.com/tektoncd/pipeline/pkg/names"
	"go.uber.org/zap"
	"gomodules.xyz/jsonpatch/v2"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"

	"github.com/hashicorp/go-multierror"
	clientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	listersalpha "github.com/tektoncd/pipeline/pkg/client/listers/pipeline/v1alpha1"
	listers "github.com/tektoncd/pipeline/pkg/client/listers/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/pkg/reconciler/events"
	exceptionclientset "github.com/vincentpli/exception-handler/pkg/client/clientset/versioned"
	listersexception "github.com/vincentpli/exception-handler/pkg/client/listers/exception/v1alpha1"
	"knative.dev/pkg/logging"

	"github.com/tektoncd/pipeline/pkg/apis/pipeline"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	v1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	runreconciler "github.com/tektoncd/pipeline/pkg/client/injection/reconciler/pipeline/v1alpha1/run"
	"github.com/tektoncd/pipeline/pkg/termination"
	"github.com/vincentpli/exception-handler/pkg/apis/exception"
	exceptionv1alpha1 "github.com/vincentpli/exception-handler/pkg/apis/exception/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//"github.com/tektoncd/pipeline/pkg/names"
)

// Reconciler implements addressableservicereconciler.Interface for
// AddressableService resources.
type Reconciler struct {
	// Tracker builds an index of what resources are watching other resources
	// so that we can immediately react to changes tracked resources.
	Tracker tracker.Interface

	//Clientset about resources
	pipelineClientSet  clientset.Interface
	exceptionClientSet exceptionclientset.Interface

	// Listers index properties about resources
	runLister         listersalpha.RunLister
	exceptionLister   listersexception.ExceptionLister
	pipelineRunLister listers.PipelineRunLister
}

// Check that our Reconciler implements Interface
var _ runreconciler.Interface = (*Reconciler)(nil)
var cancelPipelineRunPatchBytes []byte

const (
	// PIPELINERUN_NAME is the param key which will deliveied by RUN
	PIPELINERUNNAME = "pipelinerun_name"
	// PREFIX help to decide if the task is a job activity one
	PREFIX = "job-activity-"
	// ERRSOURCE is the key of params which will be deliver to exception handler pipelinerun
	ERRSOURCE = "err-source"
	// ERRNUMBER is the key of params which will be deliver to exception handler pipelinerun
	ERRNUMBER = "err-number"
	// ERRMESSAGE is the key of params which will be deliver to exception handler pipelinerun
	ERRMESSAGE = "err-message"
)

func init() {
	var err error
	cancelPipelineRunPatchBytes, err = json.Marshal([]jsonpatch.JsonPatchOperation{{
		Operation: "add",
		Path:      "/spec/status",
		Value:     v1beta1.PipelineRunSpecStatusCancelled,
	}})
	if err != nil {
		log.Fatalf("failed to marshal PipelineRun cancel patch bytes: %v", err)
	}
}

type exceptionResult struct {
	TaskrunName string
	ErrSource   string
	ErrNumber   string
	ErrMessage  string
}

// ReconcileKind implements Interface.ReconcileKind.
func (r *Reconciler) ReconcileKind(ctx context.Context, run *v1alpha1.Run) reconciler.Event {
	var merr error
	logger := logging.FromContext(ctx)
	logger.Infof("Reconciling Run %s/%s at %v", run.Namespace, run.Name, time.Now())

	// Check that the Run references a Exception CRD.  The logic is controller.go should ensure that only this type of Run
	// is reconciled this controller but it never hurts to do some bullet-proofing.
	if run.Spec.Ref == nil ||
		run.Spec.Ref.APIVersion != exceptionv1alpha1.SchemeGroupVersion.String() ||
		run.Spec.Ref.Kind != "Exception" {
		logger.Errorf("Received control for a Run %s/%s that does not reference a Exception custom CRD", run.Namespace, run.Name)
		return nil
	}

	// If the Run has not started, initialize the Condition and set the start time.
	if !run.HasStarted() {
		logger.Infof("Starting new Run %s/%s", run.Namespace, run.Name)
		run.Status.InitializeConditions()
		// In case node time was not synchronized, when controller has been scheduled to other nodes.
		if run.Status.StartTime.Sub(run.CreationTimestamp.Time) < 0 {
			logger.Warnf("Run %s createTimestamp %s is after the Run started %s", run.Name, run.CreationTimestamp, run.Status.StartTime)
			run.Status.StartTime = &run.CreationTimestamp
		}

		// Emit events. During the first reconcile the status of the Run may change twice
		// from not Started to Started and then to Running, so we need to sent the event here
		// and at the end of 'Reconcile' again.
		// We also want to send the "Started" event as soon as possible for anyone who may be waiting
		// on the event to perform user facing initialisations, such has reset a CI check status
		afterCondition := run.Status.GetCondition(apis.ConditionSucceeded)
		events.Emit(ctx, nil, afterCondition, run)
	}

	if run.IsDone() {
		logger.Infof("Run %s/%s is done", run.Namespace, run.Name)
		return nil
	}

	if run.IsCancelled() {
		err := r.cancelExceptionHandler(ctx, run, logger)
		if err != nil {
			run.Status.MarkRunFailed(exceptionv1alpha1.ExceptionRunReasonCouldntCancel.String(),
				"Cancel exception hander failed: %v", err)
			logger.Errorf("Cancel exception hander failed: %v", err.Error())
		}

		return nil
	}

	// Store the condition before reconcile
	beforeCondition := run.Status.GetCondition(apis.ConditionSucceeded)

	status := &exceptionv1alpha1.ExceptionStatus{}
	if err := run.Status.DecodeExtraFields(status); err != nil {
		run.Status.MarkRunFailed(exceptionv1alpha1.ExceptionRunReasonInternalError.String(),
			"Internal error calling DecodeExtraFields: %v", err)
		logger.Errorf("DecodeExtraFields error: %v", err.Error())
	}

	// Reconcile the Run
	if err := r.reconcile(ctx, run, status); err != nil {
		logger.Errorf("Reconcile error: %v", err.Error())
		merr = multierror.Append(merr, err)
	}

	if err := r.updateLabelsAndAnnotations(ctx, run); err != nil {
		logger.Warn("Failed to update Run labels/annotations", zap.Error(err))
		merr = multierror.Append(merr, err)
	}

	if err := run.Status.EncodeExtraFields(status); err != nil {
		run.Status.MarkRunFailed(exceptionv1alpha1.ExceptionRunReasonInternalError.String(),
			"Internal error calling EncodeExtraFields: %v", err)
		logger.Errorf("EncodeExtraFields error: %v", err.Error())
	}

	afterCondition := run.Status.GetCondition(apis.ConditionSucceeded)
	events.Emit(ctx, beforeCondition, afterCondition, run)

	// Only transient errors that should retry the reconcile are returned.
	return merr

}

func (r *Reconciler) cancelExceptionHandler(ctx context.Context, run *v1alpha1.Run, logger *zap.SugaredLogger) error {
	// Get the Exception referenced by the Run
	exception, err := r.getException(ctx, run)
	if err != nil {
		return nil
	}

	if exception.Status.PipelineName == "" {
		logger.Info("Exception handler PR not existed, ignore")
		return nil
	}

	if _, err := r.pipelineClientSet.TektonV1beta1().PipelineRuns(run.Namespace).Patch(ctx, exception.Status.PipelineName, types.JSONPatchType, cancelPipelineRunPatchBytes, metav1.PatchOptions{}, ""); err != nil {
		return err
	}

	return nil
}

func (r *Reconciler) reconcile(ctx context.Context, run *v1alpha1.Run, status *exceptionv1alpha1.ExceptionStatus) error {
	logger := logging.FromContext(ctx)

	// Get the Exception referenced by the Run
	exception, err := r.getException(ctx, run)
	if err != nil {
		return nil
	}

	status = exception.Status.DeepCopy()
	if exception.Status.PipelineName == "" {
		// Store the fetched exceptionSpec on the Run for auditing
		storeExceptionSpec(status, &exception.Spec)

		// Propagate labels and annotations from Exception to Run.
		propagateExceptionLabelsAndAnnotations(run, &exception.ObjectMeta)

		// Check if we need do exception handle, if no need, mark Run Done
		exceptionRes, wait, err := r.getOriginalPipelinerun(ctx, run, logger)
		if wait {
			run.Status.MarkRunRunning(exceptionv1alpha1.ExceptionRunReasonRunning.String(),
				"There is no taskrun in original pr mark as failed, wait: %s", time.Now().String())
			return nil
		}

		if err != nil {
			run.Status.MarkRunFailed(exceptionv1alpha1.ExceptionRunReasonCouldntGetOriginalPipelinerun.String(),
				"Could not get original Pipelinerun: %w", err)

			return nil
		}

		if exceptionRes == nil {
			run.Status.MarkRunSucceeded(exceptionv1alpha1.ExceptionRunReasonSucceeded.String(),
				"No exception handler, since all task success")

			return nil
		}

		// Create PipelineRun if not existed
		pr, err := r.createPipelinerun(ctx, exceptionRes, &exception.Spec, &exception.ObjectMeta, run, logger)
		if err != nil {
			run.Status.MarkRunFailed(exceptionv1alpha1.ExceptionRunReasonCoundntCreate.String(),
				"Could not create new Pipelinerun: %w", err)
			return nil
		}

		status.PipelineName = pr.Name

		run.Status.MarkRunRunning(exceptionv1alpha1.ExceptionRunReasonRunning.String(),
			"Exception handler pipelinerun %s is created", pr.Name)
	} else {
		// Check the status of Pipelinerun, if complete, update the status of Run as Done.
		//status = exception.Status.DeepCopy()
		exceptionHandlerPr, err := r.retrievalExceptionHandlerPr(ctx, run, exception.Status.PipelineName)
		if err != nil {
			return fmt.Errorf("Get Exception Handler Pipelinerun: %s failed: %w", fmt.Sprintf("%s/%s", run.Namespace, exception.Status.PipelineName), err)
		}

		if exceptionHandlerPr.IsDone() {
			run.Status.MarkRunSucceeded(exceptionv1alpha1.ExceptionRunReasonSucceeded.String(),
				"Exception handler done")

		} else {
			run.Status.MarkRunRunning(exceptionv1alpha1.ExceptionRunReasonRunning.String(),
				"Exception handler pipelinerun %s is running", exception.Status.PipelineName)
		}

		status.PipelineRunStatus = &exceptionHandlerPr.Status
	}

	exception.Status = *status.DeepCopy()
	_, err = r.exceptionClientSet.CustomV1alpha1().Exceptions(run.Namespace).UpdateStatus(ctx, exception, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("Update status of Exception: %s failed: %w", fmt.Sprintf("%s/%s", exception.Namespace, exception.Name), err)
	}

	return nil
}

func (r *Reconciler) retrievalExceptionHandlerPr(ctx context.Context, run *v1alpha1.Run, prName string) (*v1beta1.PipelineRun, error) {
	return r.pipelineClientSet.TektonV1beta1().PipelineRuns(run.Namespace).Get(ctx, prName, metav1.GetOptions{})
}

func (r *Reconciler) getOriginalPipelinerun(ctx context.Context, run *v1alpha1.Run, logger *zap.SugaredLogger) (*exceptionResult, bool, error) {
	var result *exceptionResult

	param := run.Spec.GetParam(PIPELINERUNNAME)
	if param == nil {
		return result, false, fmt.Errorf("Missing spec.Params[0].pipelinerun_name for Run %s", fmt.Sprintf("%s/%s", run.Namespace, run.Name))
	}

	pr, err := r.pipelineClientSet.TektonV1beta1().PipelineRuns(run.Namespace).Get(ctx, param.Value.StringVal, metav1.GetOptions{})
	if err != nil {
		return result, false, fmt.Errorf("Get Pipelinerun: %s failed: %w", fmt.Sprintf("%s/%s", run.Namespace, param.Name), err)
	}
	/*
		loop the Pipelinerun.Status.TaskRuns
		1. If the Pipelinerun.Status.TaskRuns[x].PipelineTaskName not prefix as "job-activity", ignore, that's means the task is not a Job Avtivity.
		2. Pipelinerun.Status.TaskRuns[x].Status.Conditions[0].type == Succeeded and Pipelinerun.Status.TaskRuns[x].Status.Conditions[0].status == True, means task success.
	*/
	wait := false
	for _, tr := range pr.Status.TaskRuns {
		if !strings.HasPrefix(tr.PipelineTaskName, PREFIX) {
			continue
		}

		if tr.Status.GetCondition(apis.ConditionSucceeded).IsUnknown() {
			wait = true
		}

		if tr.Status.GetCondition(apis.ConditionSucceeded).IsFalse() {
			//the task failed, need write the failed reason to result
			// A exception result should be: [{name: ErrSource, value: xxx}, {name: ErrNumber, value: xxx}, {name: ErrMessage, value: xxx}]
			result, err = parseRes(tr, logger)
			if err != nil {
				logger.Errorf("Parse state of steps failed: %v", err)
				return nil, false, err
			}

			//expect only one exception raised
			return result, false, nil
		}
	}

	return result, wait, nil
}

/* By design of Tekton, the failed Task will not report it's result to Status of Taskrun, see the discussion:
   https://github.com/tektoncd/pipeline/issues/3439

   cannot wait community, we will parse Result from Step now
*/
func parseRes(trStatus *v1beta1.PipelineRunTaskRunStatus, logger *zap.SugaredLogger) (*exceptionResult, error) {
	var result *exceptionResult

	stepState := trStatus.Status.Steps
	if stepState == nil || len(stepState) == 0 {
		return nil, fmt.Errorf("No step status found")
	}

	for _, state := range stepState {
		if state.Terminated != nil && len(state.Terminated.Message) != 0 {
			msg := state.Terminated.Message
			results, err := termination.ParseMessage(logger, msg)

			if err != nil {
				logger.Errorf("termination message could not be parsed as JSON: %v", err)

				return nil, fmt.Errorf("termination message could not be parsed as JSON: %v", err)
			} else {
				result = &exceptionResult{}
				for _, r := range results {
					if r.ResultType == v1beta1.TaskRunResultType {
						switch r.Key {
						case ERRSOURCE:
							result.ErrSource = r.Value
						case ERRNUMBER:
							result.ErrNumber = r.Value
						case ERRMESSAGE:
							result.ErrMessage = r.Value
						}
					}
				}
				// assumption only on step in a task
				break
			}
		}
	}

	return result, nil
}

func (r *Reconciler) createPipelinerun(ctx context.Context, results *exceptionResult, exs *exceptionv1alpha1.ExceptionSpec, exceptionMeta *metav1.ObjectMeta, run *v1alpha1.Run, logger *zap.SugaredLogger) (*v1beta1.PipelineRun, error) {
	manifest := exs.PipelineRunSpec
	if manifest == nil {
		return nil, fmt.Errorf("Missing spec.PipelineRunSpec for Exception %s", fmt.Sprintf("%s/%s", exceptionMeta.Namespace, exceptionMeta.Name))
	}
	// Create name for TaskRun from Run name plus iteration number.
	prName := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix(fmt.Sprintf("%s", run.Name))

	pr := &v1beta1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:            prName,
			Namespace:       run.Namespace,
			OwnerReferences: []metav1.OwnerReference{run.GetOwnerReference()},
			Labels:          getTaskRunLabels(run),
			Annotations:     getPipelineRunAnnotations(run),
		},
		Spec: *manifest,
	}
	// create params  TODO
	params := []v1beta1.Param{
		{
			Name:  "ErrSource",
			Value: *v1beta1.NewArrayOrString(results.ErrSource),
		},
		{
			Name:  "ErrMessage",
			Value: *v1beta1.NewArrayOrString(results.ErrMessage),
		},
		{
			Name:  "ErrNumber",
			Value: *v1beta1.NewArrayOrString(results.ErrNumber),
		},
	}

	pr.Spec.Params = params
	logger.Infof("Creating a new Pipelinerun object %s", prName)
	return r.pipelineClientSet.TektonV1beta1().PipelineRuns(run.Namespace).Create(ctx, pr, metav1.CreateOptions{})
}

func (r *Reconciler) getException(ctx context.Context, run *v1alpha1.Run) (*exceptionv1alpha1.Exception, error) {
	var exception *exceptionv1alpha1.Exception

	if run.Spec.Ref != nil && run.Spec.Ref.Name != "" {
		// Use the k8 client to get the TaskLoop rather than the lister.  This avoids a timing issue where
		// the TaskLoop is not yet in the lister cache if it is created at nearly the same time as the Run.
		// See https://github.com/tektoncd/pipeline/issues/2740 for discussion on this issue.
		//
		// tl, err := c.taskLoopLister.TaskLoops(run.Namespace).Get(run.Spec.Ref.Name)
		ex, err := r.exceptionClientSet.CustomV1alpha1().Exceptions(run.Namespace).Get(ctx, run.Spec.Ref.Name, metav1.GetOptions{})
		if err != nil {
			run.Status.MarkRunFailed(exceptionv1alpha1.ExceptionRunReasonCouldntGet.String(),
				"Error retrieving TaskLoop for Run %s/%s: %s",
				run.Namespace, run.Name, err)
			return nil, fmt.Errorf("Error retrieving Exception for Run %s: %w", fmt.Sprintf("%s/%s", run.Namespace, run.Name), err)
		}

		exception = ex
	} else {
		// Run does not require name but for TaskLoop it does.
		run.Status.MarkRunFailed(exceptionv1alpha1.ExceptionRunReasonCouldntGet.String(),
			"Missing spec.ref.name for Run %s/%s",
			run.Namespace, run.Name)
		return nil, fmt.Errorf("Missing spec.ref.name for Run %s", fmt.Sprintf("%s/%s", run.Namespace, run.Name))
	}

	return exception, nil
}

func (r *Reconciler) updateLabelsAndAnnotations(ctx context.Context, run *v1alpha1.Run) error {
	newRun, err := r.runLister.Runs(run.Namespace).Get(run.Name)
	if err != nil {
		return fmt.Errorf("error getting Run %s when updating labels/annotations: %w", run.Name, err)
	}
	if !reflect.DeepEqual(run.ObjectMeta.Labels, newRun.ObjectMeta.Labels) || !reflect.DeepEqual(run.ObjectMeta.Annotations, newRun.ObjectMeta.Annotations) {
		mergePatch := map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels":      run.ObjectMeta.Labels,
				"annotations": run.ObjectMeta.Annotations,
			},
		}
		patch, err := json.Marshal(mergePatch)
		if err != nil {
			return err
		}
		_, err = r.pipelineClientSet.TektonV1alpha1().Runs(run.Namespace).Patch(ctx, run.Name, types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	}
	return nil
}

func storeExceptionSpec(status *exceptionv1alpha1.ExceptionStatus, exs *exceptionv1alpha1.ExceptionSpec) {
	// Only store the ExceptionSpec once, if it has never been set before.
	if status.ExceptionSpec == nil {
		status.ExceptionSpec = exs
	}
}

func propagateExceptionLabelsAndAnnotations(run *v1alpha1.Run, exceptionMeta *metav1.ObjectMeta) {
	// Propagate labels from TaskLoop to Run.
	if run.ObjectMeta.Labels == nil {
		run.ObjectMeta.Labels = make(map[string]string, len(exceptionMeta.Labels)+1)
	}
	for key, value := range exceptionMeta.Labels {
		run.ObjectMeta.Labels[key] = value
	}
	run.ObjectMeta.Labels[exception.GroupName+"/exception"] = exceptionMeta.Name

	// Propagate annotations from TaskLoop to Run.
	if run.ObjectMeta.Annotations == nil {
		run.ObjectMeta.Annotations = make(map[string]string, len(exceptionMeta.Annotations))
	}
	for key, value := range exceptionMeta.Annotations {
		run.ObjectMeta.Annotations[key] = value
	}
}

func getPipelineRunAnnotations(run *v1alpha1.Run) map[string]string {
	// Propagate annotations from Run to TaskRun.
	annotations := make(map[string]string, len(run.ObjectMeta.Annotations)+1)
	for key, val := range run.ObjectMeta.Annotations {
		annotations[key] = val
	}
	return annotations
}

func getTaskRunLabels(run *v1alpha1.Run) map[string]string {
	// Propagate labels from Run to TaskRun.
	labels := make(map[string]string, len(run.ObjectMeta.Labels)+1)
	for key, val := range run.ObjectMeta.Labels {
		labels[key] = val
	}
	// Note: The Run label uses the normal Tekton group name.
	labels[pipeline.GroupName+"/run"] = run.Name
	return labels
}

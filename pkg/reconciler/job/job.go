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

package job

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"

	"github.com/hashicorp/go-multierror"
	clientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	listersalpha "github.com/tektoncd/pipeline/pkg/client/listers/pipeline/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/reconciler/events"
	"knative.dev/pkg/logging"

	"net/http"

	"github.com/tektoncd/pipeline/pkg/apis/pipeline"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	runreconciler "github.com/tektoncd/pipeline/pkg/client/injection/reconciler/pipeline/v1alpha1/run"
	"github.com/vincentpli/concurrent-reconcile/pkg/apis/job"
	jobv1alpha1 "github.com/vincentpli/concurrent-reconcile/pkg/apis/job/v1alpha1"
	jobclientset "github.com/vincentpli/concurrent-reconcile/pkg/client/clientset/versioned"
	listersjob "github.com/vincentpli/concurrent-reconcile/pkg/client/listers/job/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reconciler implements addressableservicereconciler.Interface for
// AddressableService resources.
type Reconciler struct {
	// Tracker builds an index of what resources are watching other resources
	// so that we can immediately react to changes tracked resources.
	Tracker tracker.Interface

	//Clientset about resources
	pipelineClientSet clientset.Interface
	jobClientSet      jobclientset.Interface

	// Listers index properties about resources
	runLister listersalpha.RunLister
	jobLister listersjob.JobLister
}

// Check that our Reconciler implements Interface
var _ runreconciler.Interface = (*Reconciler)(nil)

const (
	//BASEURL is
	BASEURL = "https://api.dataplatform.dev.cloud.ibm.com"
	//PROJECTID is the param key which will deliveied by RUN
	PROJECTID = "project_id"
	//SPACEID is the param key which will deliveied by RUN
	SPACEID = "space_id"
	//JOBID is the param key which will deliveied by RUN
	JOBID = "job_id"
	//INVOKETOKEN is the param key which will deliveied by RUN
	INVOKETOKEN = "token"
)

type invokeParams struct {
	ProjectID   string
	SpaceID     string
	JobID       string
	InvokeToken string
}

type Result struct {
	Metadatas Metadata `json:"metadata,omitempty"`
}

type Metadata struct {
	AssetID string `json:"asset_id,omitempty"`
}

type result struct {
	RunID string
}

func init() {
}

// ReconcileKind implements Interface.ReconcileKind.
func (r *Reconciler) ReconcileKind(ctx context.Context, run *v1alpha1.Run) reconciler.Event {
	var merr error
	logger := logging.FromContext(ctx)
	logger.Infof("Reconciling Run %s/%s at %v", run.Namespace, run.Name, time.Now())

	//Fake check
	if run.Status.StartTime != nil && time.Now().Sub(run.Status.StartTime.Time) < 5*time.Second {
		fmt.Println("------------------------------------------- raise fake error")
		fmt.Println(time.Now().Sub(run.Status.StartTime.Time))
		merr = multierror.Append(merr, fmt.Errorf("fake error to requeue the work queue"))
		return merr
	}
	// Check that the Run references a Exception CRD.  The logic is controller.go should ensure that only this type of Run
	// is reconciled this controller but it never hurts to do some bullet-proofing.
	if run.Spec.Ref == nil ||
		run.Spec.Ref.APIVersion != jobv1alpha1.SchemeGroupVersion.String() ||
		run.Spec.Ref.Kind != "Job" {
		logger.Errorf("Received control for a Run %s/%s that does not reference a Job custom CRD", run.Namespace, run.Name)
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
		return nil
	}

	// Store the condition before reconcile
	beforeCondition := run.Status.GetCondition(apis.ConditionSucceeded)

	// Reconcile the Run
	if err := r.reconcile(ctx, run); err != nil {
		logger.Errorf("Reconcile error: %v", err.Error())
		merr = multierror.Append(merr, err)
	}

	if err := r.updateLabelsAndAnnotations(ctx, run); err != nil {
		logger.Warn("Failed to update Run labels/annotations", zap.Error(err))
		merr = multierror.Append(merr, err)
	}

	afterCondition := run.Status.GetCondition(apis.ConditionSucceeded)
	events.Emit(ctx, beforeCondition, afterCondition, run)

	// Only transient errors that should retry the reconcile are returned.
	return merr

}

func (r *Reconciler) reconcile(ctx context.Context, run *v1alpha1.Run) error {
	logger := logging.FromContext(ctx)

	projectID := run.Spec.GetParam(PROJECTID)
	if projectID == nil {
		logger.Errorf("Missing spec.Params[0].project_id for Run %s", fmt.Sprintf("%s/%s", run.Namespace, run.Name))
		run.Status.MarkRunFailed(jobv1alpha1.JobRunReasonCouldntGetParam.String(),
			"Could not get param: %v", PROJECTID)

		return nil
	}
	spaceID := run.Spec.GetParam(SPACEID)
	if spaceID == nil {
		logger.Errorf("Missing spec.Params[0].space_id for Run %s", fmt.Sprintf("%s/%s", run.Namespace, run.Name))
		run.Status.MarkRunFailed(jobv1alpha1.JobRunReasonCouldntGetParam.String(),
			"Could not get param: %v", SPACEID)

		return nil
	}
	jobID := run.Spec.GetParam(JOBID)
	if jobID == nil {
		logger.Errorf("Missing spec.Params[0].job_id for Run %s", fmt.Sprintf("%s/%s", run.Namespace, run.Name))
		run.Status.MarkRunFailed(jobv1alpha1.JobRunReasonCouldntGetParam.String(),
			"Could not get param: %v", JOBID)

		return nil
	}
	invokeToken := run.Spec.GetParam(INVOKETOKEN)
	if invokeToken == nil {
		return fmt.Errorf("Missing spec.Params[0].invokeToken for Run %s", fmt.Sprintf("%s/%s", run.Namespace, run.Name))
		run.Status.MarkRunFailed(jobv1alpha1.JobRunReasonCouldntGetParam.String(),
			"Could not get param: %v", INVOKETOKEN)

		return nil
	}

	params := invokeParams{
		ProjectID:   projectID.Value.StringVal,
		SpaceID:     spaceID.Value.StringVal,
		JobID:       jobID.Value.StringVal,
		InvokeToken: invokeToken.Value.StringVal,
	}

	runID, err := sendFakeRequest(params, logger)
	if err != nil {
		logger.Errorf("Send request hit failed: %v", err)
	}

	result := result{RunID: runID}
	if err := run.Status.EncodeExtraFields(result); err != nil {
		run.Status.MarkRunFailed(jobv1alpha1.JobRunReasonInternalError.String(),
			"Internal error calling EncodeExtraFields: %v", err)
		logger.Errorf("EncodeExtraFields error: %v", err.Error())
	} else {
	/*	fmt.Println("hahahahahah")
		fmt.Println(run.Status.GetCondition(apis.ConditionSucceeded).Status)
		if run.Status.GetCondition(apis.ConditionSucceeded).IsUnknown() {
			run.Status.MarkRunSucceeded(jobv1alpha1.JobRunReasonSuccess.String(),
				"Send request success")
		} else {
			run.Status.MarkRunRunning(jobv1alpha1.JobRunReasonSuccess.String(),
				"Send request keepping")
		}
	*/
	  run.Status.StartTime.Time = time.Now()
	}

	return nil
}

func sendFakeRequest(params invokeParams, logger *zap.SugaredLogger) (string, error) {
	logger.Infof("Start send fake request...")
	time.Sleep(2 * time.Second)
	logger.Infof("finish send fake request...")
	return "", nil
}

func sendRequest(params invokeParams, logger *zap.SugaredLogger) (string, error) {
	logger.Infof("Start create job run with: project_id: %s, space_id: %s, job_id: %s ", params.ProjectID, params.SpaceID, params.JobID)

	var requstBody = []byte(`{"job_run":{}}`)

	timeout := time.Duration(10 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	runScope := generateRunscope(params)
	url := fmt.Sprintf(BASEURL+"/v2/jobs/%s/runs?%s", params.JobID, runScope)

	request, err := http.NewRequest("POST", url, bytes.NewBuffer(requstBody))
	if err != nil {
		logger.Errorf("create request hit error: %v", err)
		return "", err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+params.InvokeToken)

	resp, err := client.Do(request)
	if err != nil {
		logger.Errorf("send request hit error: %v", err)
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode == 201 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			logger.Errorf("read response hit error: %v", err)
			return "", err
		}

		// var respObj map[string]interface{}
		respObj := &Result{}
		err = json.Unmarshal(body, respObj)
		if err != nil {
			logger.Errorf("parse response hit error: %v", err)
			return "", err
		}

		// return fmt.Sprintf("%v", respObj["metadata"].(map[string]interface{})["asset_id"]), nil
		return respObj.Metadatas.AssetID, nil
	}

	return "", fmt.Errorf("Get error status code: %s, from: %v", resp.StatusCode, resp)
}

func generateRunscope(params invokeParams) string {
	var scope = ""
	if params.ProjectID != "" {
		scope += "&project_id=" + params.ProjectID
	}
	if params.SpaceID != "" {
		scope += "&space_id=" + params.SpaceID
	}

	if strings.HasPrefix(scope, "&") {
		scope = scope[1:len(scope)]
	}

	return scope
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

func propagateExceptionLabelsAndAnnotations(run *v1alpha1.Run, exceptionMeta *metav1.ObjectMeta) {
	// Propagate labels from TaskLoop to Run.
	if run.ObjectMeta.Labels == nil {
		run.ObjectMeta.Labels = make(map[string]string, len(exceptionMeta.Labels)+1)
	}
	for key, value := range exceptionMeta.Labels {
		run.ObjectMeta.Labels[key] = value
	}
	run.ObjectMeta.Labels[job.GroupName+"/job"] = exceptionMeta.Name

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

/*
Copyright 2019 The Knative Authors.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
)

// +genclient
// +genreconciler
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Job is a global exception handler which will handle any exception in a certain pipeline
type Job struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the AddressableService (from the client).
	// +optional
	Spec JobSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the AddressableService (from the controller).
	// +optional
	Status JobStatus `json:"status,omitempty"`
}

var (
	// Check that AddressableService can be validated and defaulted.
	_ apis.Validatable   = (*Job)(nil)
	_ apis.Defaultable   = (*Job)(nil)
	_ kmeta.OwnerRefable = (*Job)(nil)
	// Check that the type conforms to the duck Knative Resource shape.
	_ duckv1.KRShaped = (*Job)(nil)
)

// JobSpec holds the desired state of the AddressableService (from the client).
type JobSpec struct {
	// PipelineRunSpec holds the object which will be create in concile
	Dummy string `json:"dummy"`
}

// JobRunReason represents a reason for the Run "Succeeded" condition
type JobRunReason string

const (
	// ExceptionConditionReady is set when the revision is starting to materialize
	// runtime resources, and becomes true when those resources are ready.
	ExceptionConditionReady = apis.ConditionReady
	// ExceptionRunReasonInternalError indicates that the Exception failed due to an internal error in the reconciler
	ExceptionRunReasonInternalError JobRunReason = "ExceptionInternalError"
	// ExceptionRunReasonCouldntCancel indicates that the Exception failed due to an internal error in the reconciler
	ExceptionRunReasonCouldntCancel JobRunReason = "CouldntCancel"
	// ExceptionRunReasonCouldntGet indicates that the associated Exception couldn't be retrieved
	ExceptionRunReasonCouldntGet JobRunReason = "CouldntGet"
	// ExceptionRunReasonCouldntGetOriginalPipelinerun indicates that the associated Exception couldn't be retrieved
	ExceptionRunReasonCouldntGetOriginalPipelinerun JobRunReason = "CouldntGetOriginalPipelinerun"
	// ExceptionRunReasonNoError indicates that the original Pipelinerun has no error
	ExceptionRunReasonNoError JobRunReason = "NoError"
	// ExceptionRunReasonCoundntCreate indicates that could not create new pipelinerun
	ExceptionRunReasonCoundntCreate JobRunReason = "CoundntCreate"
	// ExceptionRunReasonRunning indicates that the new created pipelinerun is running
	ExceptionRunReasonRunning JobRunReason = "Running"
	// ExceptionRunReasonSucceeded indicates that created Pipelinerun success or no error in original Pipelinerun
	ExceptionRunReasonSucceeded JobRunReason = "Succeeded"
)

func (e JobRunReason) String() string {
	return string(e)
}

// JobStatus communicates the observed state of the AddressableService (from the controller).
type JobStatus struct {
	duckv1.Status `json:",inline"`
	// JobRunID contains the exact spec used to instantiate the Run
	JobRunID string `json:"jobrunid,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// JobList is a list of AddressableService resources
type JobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Job `json:"items"`
}

// GetStatus retrieves the status of the resource. Implements the KRShaped interface.
func (ex *Job) GetStatus() *duckv1.Status {
	return &ex.Status.Status
}

module github.com/vincentpli/concurrent-reconcile

go 1.13

require (
	github.com/google/licenseclassifier v0.0.0-20200708223521-3d09a0ea2f39
	github.com/hashicorp/go-multierror v1.1.0
	github.com/tektoncd/pipeline v0.18.1
	go.uber.org/zap v1.15.0
	gomodules.xyz/jsonpatch/v2 v2.1.0
	k8s.io/api v0.18.8
	k8s.io/apiextensions-apiserver v0.18.8 // indirect
	k8s.io/apimachinery v0.19.0
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/code-generator v0.18.8
	k8s.io/kube-openapi v0.0.0-20200410145947-bcb3869e6f29
	knative.dev/hack v0.0.0-20201027201633-1763a666eb41
	knative.dev/pkg v0.0.0-20201020033659-eafc8c6f72a7
	knative.dev/sample-controller v0.0.0-20201120182152-9962c006df5e
)

replace (
	k8s.io/api => k8s.io/api v0.18.8
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.18.8
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.8
	k8s.io/apiserver => k8s.io/apiserver v0.18.8
	k8s.io/client-go => k8s.io/client-go v0.18.8
	k8s.io/code-generator => k8s.io/code-generator v0.18.8
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20200410145947-61e04a5be9a6
	knative.dev/pkg => knative.dev/pkg v0.0.0-20200922164940-4bf40ad82aab
)
module github.com/vincentpli/concurrent-reconcile

go 1.13

require (
	github.com/google/licenseclassifier v0.0.0-20200708223521-3d09a0ea2f39
	github.com/hashicorp/go-multierror v1.1.0
	github.com/tektoncd/pipeline v0.18.1
	go.uber.org/zap v1.16.0
	k8s.io/api v0.19.7
	k8s.io/apimachinery v0.19.7
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/code-generator v0.19.7
	k8s.io/kube-openapi v0.0.0-20200410145947-bcb3869e6f29
	knative.dev/hack v0.0.0-20210120165453-8d623a0af457
	knative.dev/pkg v0.0.0-20210107022335-51c72e24c179
	knative.dev/sample-controller v0.0.0-20210118144022-bd83d56f4501
	knative.dev/test-infra v0.0.0-20201020062259-cd8625126729 // indirect
)

replace (
	k8s.io/api => k8s.io/api v0.18.8
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.18.8
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.8
	k8s.io/apiserver => k8s.io/apiserver v0.18.8
	k8s.io/client-go => k8s.io/client-go v0.18.8
	k8s.io/code-generator => k8s.io/code-generator v0.18.8
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20200410145947-61e04a5be9a6
	knative.dev/pkg => knative.dev/pkg v0.0.0-20210127163530-0d31134d5f4e
)

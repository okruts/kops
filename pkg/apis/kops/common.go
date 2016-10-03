package kops

import (
	"k8s.io/kubernetes/pkg/runtime"
)

type ApiType interface {
	runtime.Object

	Validate() error
}

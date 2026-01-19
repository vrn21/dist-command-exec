// Package v1 contains API types for the distexec.io API group.
package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is the API group and version for this package.
	GroupVersion = schema.GroupVersion{Group: "distexec.io", Version: "v1"}

	// SchemeGroupVersion is an alias for GroupVersion for compatibility.
	SchemeGroupVersion = GroupVersion
)

// Resource returns the GroupResource for a given resource name.
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}

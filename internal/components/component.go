/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package components

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Component struct {
	Name          string
	Subcomponents SubcomponentList
}

type Subcomponent struct {
	Name     string
	Object   client.Object
	MutateFn controllerutil.MutateFn
}

type SubcomponentList []Subcomponent

// GetName returns the name of the component
func (c *Component) GetName() string {
	return c.Name
}

// GetSubcomponents returns the subcomponents of the component
func (c *Component) GetSubcomponents() SubcomponentList {
	return c.Subcomponents
}

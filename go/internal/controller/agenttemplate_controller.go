/*
Copyright 2025.

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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// AgentTemplateReconciler reconciles a AgentTemplate object
type AgentTemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=kagent.kagent.dev,resources=agenttemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kagent.kagent.dev,resources=agenttemplates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kagent.kagent.dev,resources=agenttemplates/finalizers,verbs=update
//+kubebuilder:rbac:groups=kagent.kagent.dev,resources=agents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kagent.kagent.dev,resources=agents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kagent.kagent.dev,resources=agents/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AgentTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling AgentTemplate")

	// Fetch the AgentTemplate instance
	var agentTemplate v1alpha2.AgentTemplate
	if err := r.Get(ctx, req.NamespacedName, &agentTemplate); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "unable to fetch AgentTemplate")
			return ctrl.Result{}, err
		}
		logger.Info("AgentTemplate resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}

	// For now, we'll just log that the resource was reconciled.
	// In the future, we can add logic here to instantiate agents from this template.
	logger.Info("Successfully reconciled AgentTemplate", "name", agentTemplate.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha2.AgentTemplate{}).
		Complete(r)
}

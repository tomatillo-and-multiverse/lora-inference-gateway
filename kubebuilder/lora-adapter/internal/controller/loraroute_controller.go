/*
Copyright 2024.

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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	inferencev1alpha1 "x-k8s.io/inference/api/v1alpha1"
)

// LLMRouteReconciler reconciles a LLMRoute object
type LLMRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=inference.x-k8s.io,resources=LLMRoutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=inference.x-k8s.io,resources=LLMRoutes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=inference.x-k8s.io,resources=LLMRoutes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *LLMRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling LLMRoute")
	var ir inferencev1alpha1.LLMRoute
	if err := r.Get(ctx, req.NamespacedName, &ir); err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("Resource doesn't exist any more.")
			return ctrl.Result{}, nil
		} else {
			logger.Error(err, "failed to read")
			return ctrl.Result{}, err
		}
	}
	logger.Info("Got LLMRoute")

	logger.Info("Reconciling HTTPRoute")
	// An HTTPRoute to route requests to an LLMRoute to the backend envoy,
	// and also adds service configuration metadata such as priority as headers.
	// The service configuration metadata will be used by envoy ext proc for
	// intelligent routing.
	if err := createOrUpdateHTTPRoute(ctx, r.Client, httpRoute(ctx, r.Scheme, &ir)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LLMRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Add the Gateway API scheme to the manager's scheme
	if err := gatewayv1.Install(mgr.GetScheme()); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&inferencev1alpha1.LLMRoute{}).
		Complete(r)
}

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
	"fmt"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var (
	envoyClusterForPodTmpl     = template.Must(template.New("envoyClusterForPod").Parse(envoyClusterForPod))
	envoyClusterForExtProcTmpl = template.Must(template.New("envoyClusterForExtProc").Parse(envoyConfigForExtProc))
)

// ServingBackendPoolReconciler reconciles a ServingBackendPool object
type ServingBackendPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=service,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=service/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=service/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ServingBackendPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling ServingBackendPool")
	sbp := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, sbp); err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("Resource doesn't exist any more.")
			return ctrl.Result{}, nil
		} else {
			logger.Error(err, "failed to read")
			return ctrl.Result{}, err
		}
	}
	logger.Info("Got ServingBackendPool")

	pl, err := r.listPods(ctx, sbp.Spec.Selector)
	if err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("Got pods for serving backend: %+v", "num_pods", len(pl.Items))
	runningPods := []*corev1.Pod{}
	for _, pod := range pl.Items {
		if pod.Status.Phase == corev1.PodRunning {
			runningPods = append(runningPods, &pod)
		}
	}
	logger.Info("Got running pods for serving backend: %+v", "num_pods", len(runningPods))

	// Deploys an Envoy with ExtProc
	logger.Info("Reconciling ext proc")
	if err := r.reconcileExtProc(ctx, sbp, runningPods); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling envoy")
	if err := r.reconcileEnvoyService(ctx, sbp, runningPods); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *ServingBackendPoolReconciler) reconcileEnvoyService(ctx context.Context, sbp *corev1.Service, pods []*corev1.Pod) error {
	config, err := envoyConfig(ctx, r.Scheme, sbp, pods)
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info(fmt.Sprintf("Envoy config: %+v", config))
	if err := createOrUpdateConfigMap(ctx, r.Client, config); err != nil {
		return err
	}
	d := envoyDeployment(ctx, r.Scheme, sbp)
	log.FromContext(ctx).Info(fmt.Sprintf("Envoy deployment: %+v", d))
	if err := createOrUpdateDeployment(ctx, r.Client, d); err != nil {
		return err
	}
	s := envoyService(ctx, r.Scheme, sbp)
	log.FromContext(ctx).Info(fmt.Sprintf("Envoy service: %+v", s))
	if err := createOrUpdateService(ctx, r.Client, s); err != nil {
		return err
	}
	return nil
}

func (r *ServingBackendPoolReconciler) reconcileExtProc(ctx context.Context, sbp *corev1.Service, pods []*corev1.Pod) error {
	d := extProcDeployment(ctx, r.Scheme, sbp, pods)
	log.FromContext(ctx).Info(fmt.Sprintf("ExtProc deployment: %+v", d))
	if err := createOrUpdateDeployment(ctx, r.Client, d); err != nil {
		return err
	}
	s := extProcService(ctx, r.Scheme, sbp)
	log.FromContext(ctx).Info(fmt.Sprintf("ExtProc service: %+v", s))
	return createOrUpdateService(ctx, r.Client, s)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServingBackendPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Filter services to those with label "x-k8s.io/inference-serving:true"
	matchesLabel := func(object client.Object) bool {
		// Check if the object is a Service
		sbp, ok := object.(*corev1.Service)
		if !ok {
			return false // Ignore if not a Service
		}

		// Check if the label matches
		labelValue, exists := sbp.Labels[loraServingBackendLabel]
		return exists && labelValue == "true"
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}, builder.WithPredicates(predicate.NewPredicateFuncs(matchesLabel))).
		Complete(r)
}

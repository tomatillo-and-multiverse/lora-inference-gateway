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
	"bytes"
	"context"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	inferencev1alpha1 "x-k8s.io/inference/api/v1alpha1"
)

// func (r *ServingBackendPoolReconciler) createOrUpdate(ctx context.Context, co client.Object) error {
// 	found := struct {
// 		metav1.Object
// 		obj runtime.Object
// 	}{}
// 	err := r.Get(ctx, types.NamespacedName{Namespace: co.GetNamespace(), Name: co.GetName()}, found)
// 	if err != nil {
// 		// Object doesn't exist, create it
// 		if client.IgnoreNotFound(err) == nil {
// 			err := r.Create(ctx, co)
// 			if err != nil {
// 				return err
// 			}
// 			log.FromContext(ctx).Info("Object created successfully", "name", co.GetName())
// 		} else {
// 			return err // Other error occurred
// 		}
// 	} else {
// 		// Object exists, update it
// 		err := r.Update(ctx, co)
// 		if err != nil {
// 			return err
// 		}
// 		log.FromContext(ctx).Info("Object updated successfully", "name", co.GetName())
// 	}

// 	return nil
// }

func createOrUpdateHTTPRoute(ctx context.Context, c client.Client, obj *gatewayv1.HTTPRoute) error {
	found := &gatewayv1.HTTPRoute{}
	err := c.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil {
		// HTTPRoute doesn't exist, create it
		if client.IgnoreNotFound(err) == nil {
			log.FromContext(ctx).Info("Creating HTTPRoute", "object", obj)
			err := c.Create(ctx, obj)
			if err != nil {
				return err
			}
			log.FromContext(ctx).Info("HTTPRoute created successfully", "name", obj.Name)
		} else {
			return err // Other error occurred
		}
	} else {
		// HTTPRoute exists, update it
		obj.SetResourceVersion(found.GetResourceVersion())
		log.FromContext(ctx).Info("Updating HTTPRoute", "object", obj)
		err := c.Update(ctx, obj)
		if err != nil {
			return err
		}
		log.FromContext(ctx).Info("HTTPRoute updated successfully", "name", obj.Name)
	}

	return nil
}

func createOrUpdateGateway(ctx context.Context, c client.Client, obj *gatewayv1.Gateway) error {
	found := &gatewayv1.Gateway{}
	err := c.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil {
		// Gateway doesn't exist, create it
		if client.IgnoreNotFound(err) == nil {
			err := c.Create(ctx, obj)
			if err != nil {
				return err
			}
			log.FromContext(ctx).Info("Gateway created successfully", "name", obj.Name)
		} else {
			return err // Other error occurred
		}
	} else {
		// Gateway exists, update it
		obj.SetResourceVersion(found.GetResourceVersion())
		err := c.Update(ctx, obj)
		if err != nil {
			return err
		}
		log.FromContext(ctx).Info("Gateway updated successfully", "name", obj.Name)
	}

	return nil
}

func createOrUpdateConfigMap(ctx context.Context, c client.Client, obj *corev1.ConfigMap) error {
	found := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil {
		// ConfigMap doesn't exist, create it
		if client.IgnoreNotFound(err) == nil {
			err := c.Create(ctx, obj)
			if err != nil {
				return err
			}
			log.FromContext(ctx).Info("ConfigMap created successfully", "name", obj.Name)
		} else {
			return err // Other error occurred
		}
	} else {
		// ConfigMap exists, update it
		obj.SetResourceVersion(found.GetResourceVersion())
		err := c.Update(ctx, obj)
		if err != nil {
			return err
		}
		log.FromContext(ctx).Info("ConfigMap updated successfully", "name", obj.Name)
	}

	return nil
}

func createOrUpdateService(ctx context.Context, c client.Client, obj *corev1.Service) error {
	found := &corev1.Service{}
	err := c.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil {
		// Service doesn't exist, create it
		if client.IgnoreNotFound(err) == nil {
			err := c.Create(ctx, obj)
			if err != nil {
				return err
			}
			log.FromContext(ctx).Info("Service created successfully", "name", obj.Name)
		} else {
			return err // Other error occurred
		}
	} else {
		// Service exists, update it
		obj.SetResourceVersion(found.GetResourceVersion())
		err := c.Update(ctx, obj)
		if err != nil {
			return err
		}
		log.FromContext(ctx).Info("Service updated successfully", "name", obj.Name)
	}

	return nil
}

func createOrUpdateDeployment(ctx context.Context, c client.Client, obj *appsv1.Deployment) error {
	found := &appsv1.Deployment{}
	err := c.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil {
		// Deployment doesn't exist, create it
		if client.IgnoreNotFound(err) == nil {
			err := c.Create(ctx, obj)
			if err != nil {
				return err
			}
			log.FromContext(ctx).Info("Deployment created successfully", "name", obj.Name)
		} else {
			return err // Other error occurred
		}
	} else {
		// Deployment exists, update it
		obj.SetResourceVersion(found.GetResourceVersion())
		err := c.Update(ctx, obj)
		if err != nil {
			return err
		}
		log.FromContext(ctx).Info("Deployment updated successfully", "name", obj.Name)
	}

	return nil
}

func httpRouteName(gw string) string {
	return gw + "-lora-route"
}

func httpRoute(ctx context.Context, scheme *runtime.Scheme, ir *inferencev1alpha1.LLMRoute) *gatewayv1.HTTPRoute {
	p := gatewayv1.PortNumber(80)
	gwns := gatewayv1.Namespace(ir.Spec.GatewayRef.Namespace)
	httpRoute := &gatewayv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      httpRouteName(ir.Spec.GatewayRef.Name),
			Namespace: ir.Namespace,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Namespace: &gwns,
						Name:      gatewayv1.ObjectName(ir.Spec.GatewayRef.Name),
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(envoyServiceName(ir.Spec.BackendPoolRef.Name)),
									Port: &p,
								},
							},
						},
					},
				},
			},
		},
	}
	// controllerutil.SetControllerReference(ir, httpRoute, scheme)
	return httpRoute
}

func envoyService(ctx context.Context, scheme *runtime.Scheme, sbp *corev1.Service) *corev1.Service {
	name := envoyServiceName(sbp.Name)
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sbp.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": name,
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(envoyContainerPort),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
	controllerutil.SetOwnerReference(sbp, service, scheme)
	return service
}

func extProcService(ctx context.Context, scheme *runtime.Scheme, sbp *corev1.Service) *corev1.Service {
	name := extProcServiceName(sbp.Name)
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sbp.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": name,
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       extProcContainerPort,
					TargetPort: intstr.FromInt(extProcContainerPort),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
	controllerutil.SetOwnerReference(sbp, service, scheme)
	return service
}

func envoyDeployment(ctx context.Context, scheme *runtime.Scheme, sbp *corev1.Service) *appsv1.Deployment {
	name := envoyServiceName(sbp.Name)
	var replicas int32 = 1
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sbp.Namespace,
			Labels: map[string]string{
				"app": name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "grpc-envoy",
							Image:   "istio/proxyv2:1.15.0",
							Command: []string{"/usr/local/bin/envoy"},
							Args: []string{
								"--config-path", "/etc/envoy/envoy.yaml",
								"--log-level", "trace",
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: envoyContainerPort,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "envoy-config",
									MountPath: "/etc/envoy",
									ReadOnly:  true,
								},
								// {
								// 	Name:      "certs",
								// 	MountPath: "/etc/ssl/envoy",
								// },
							},
						},
						{
							Name:    "curl",
							Image:   "curlimages/curl",
							Command: []string{"sleep", "3600"},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "envoy-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: envoyConfigNam(sbp.Name),
									},
								},
							},
						},
						// {
						// 	Name: "certs",
						// 	VolumeSource: corev1.VolumeSource{
						// 		Secret: &corev1.SecretVolumeSource{
						// 			SecretName: "envoy-certs",
						// 		},
						// 	},
						// },
					},
				},
			},
		},
	}
	controllerutil.SetOwnerReference(sbp, deployment, scheme)
	return deployment
}

func extProcDeployment(ctx context.Context, scheme *runtime.Scheme, sbp *corev1.Service, pods []*corev1.Pod) *appsv1.Deployment {
	name := extProcServiceName(sbp.Name)
	podNames := make([]string, 0, len(pods))
	podAddresses := make([]string, 0, len(pods))
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
		podAddresses = append(podAddresses, pod.Status.PodIP+":"+modelServerPort)
	}
	var replicas int32 = 1
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sbp.Namespace,
			Labels: map[string]string{
				"app": name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							// TODO: Pass backend pool pod selector to the ext proc container
							Name:  "ext-proc",
							Image: extProcContainer,
							Args: []string{
								"-pods", strings.Join(podNames, ","),
								"-podIPs", strings.Join(podAddresses, ","),
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: extProcContainerPort,
								},
							},
						},
						{
							Name:    "curl",
							Image:   "curlimages/curl",
							Command: []string{"sleep", "3600"},
						},
					},
				},
			},
		},
	}
	controllerutil.SetOwnerReference(sbp, deployment, scheme)
	return deployment
}

func envoyServiceName(sbp string) string {
	return sbp + "-envoy"
}

func extProcServiceName(sbp string) string {
	return sbp + "-ext-proc"
}

func envoyConfigNam(sbp string) string {
	return sbp + "-envoy-config"
}

func envoyConfig(ctx context.Context, scheme *runtime.Scheme, sbp *corev1.Service, pods []*corev1.Pod) (*corev1.ConfigMap, error) {
	var buffer bytes.Buffer
	for _, pod := range pods {
		podData := struct {
			PodName    string
			PodAddress string
			PodPort    string
		}{
			PodName:    pod.Name,
			PodAddress: pod.Status.PodIP,
			PodPort:    modelServerPort,
		}
		log.FromContext(ctx).Info("Adding envoy cluster", "pod", podData)
		if err := envoyClusterForPodTmpl.Execute(&buffer, podData); err != nil {
			return nil, err
		}
	}
	extProcData := struct {
		ExtProcAddress string
		ExtProcPort    int
	}{
		ExtProcAddress: extProcServiceName(sbp.Name) + "." + sbp.Namespace + ".svc.cluster.local",
		ExtProcPort:    extProcContainerPort,
	}
	if err := envoyClusterForExtProcTmpl.Execute(&buffer, extProcData); err != nil {
		return nil, err
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      envoyConfigNam(sbp.Name),
			Namespace: sbp.Namespace,
		},
		Data: map[string]string{
			"envoy.yaml": staticEnvoyConfig + buffer.String(),
		},
	}
	controllerutil.SetOwnerReference(sbp, configMap, scheme)

	return configMap, nil
}

func (r *ServingBackendPoolReconciler) listPods(ctx context.Context, selector map[string]string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.MatchingLabels(selector),
	}

	if err := r.List(ctx, podList, listOpts...); err != nil {
		return nil, err
	}
	return podList, nil
}

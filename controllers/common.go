package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	cardanov1 "github.com/zenithpool/cardano-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/util/intstr"
)

func generateNodeStatefulset(name string,
	namespace string,
	image string,
	labels map[string]string,
	replicas int32,
	imagePullSecrets []corev1.LocalObjectReference,
	storage corev1.PersistentVolumeClaimSpec,
	coreNode bool) *appsv1.StatefulSet {

	state := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	state.Spec.ServiceName = name

	state.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: labels,
	}

	state.Spec.Replicas = &replicas

	state.Spec.Template.Spec.ImagePullSecrets = imagePullSecrets

	state.Spec.Template.ObjectMeta.Labels = labels
	state.Spec.Template.ObjectMeta.Annotations = map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/path":   "/metrics",
		"prometheus.io/port":   "8080",
	}

	// Create pod container details
	cardanoNode := corev1.Container{}

	cardanoNode.Name = "cardano-node"
	cardanoNode.Image = image
	cardanoNode.Ports = []corev1.ContainerPort{
		{
			ContainerPort: 31400,
			Protocol:      corev1.ProtocolTCP,
			Name:          "cardano",
		},
		{
			ContainerPort: 8080,
			Protocol:      corev1.ProtocolTCP,
			Name:          "prometheus",
		},
	}

	probe := &corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(31400),
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		FailureThreshold:    180,
	}

	// Set readiness probe
	cardanoNode.ReadinessProbe = probe

	// set livenessProbe
	cardanoNode.LivenessProbe = probe

	cardanoNode.Command = []string{"cardano-node"}
	cardanoNode.Args = []string{
		"run",
		"--config", "/configuration/configuration.yaml",
		"--database-path", "/data/db",
		"--host-addr", "0.0.0.0",
		"--port", "31400",
		"--socket-path", "/ipc/node.socket",
		"--topology", "/configuration/topology.json",
	}

	if coreNode {
		cardanoNode.Args = append(cardanoNode.Args,
			"--shelley-kes-key", "/nodeop/hot.skey",
			"--shelley-vrf-key", "/nodeop/vrf.skey",
			"--shelley-operational-certificate", "/nodeop/op.cert",
		)
	}
	cardanoNode.VolumeMounts = []corev1.VolumeMount{
		{
			Name:      "node-db",
			MountPath: "/data",
		},
		{
			Name:      "node-ipc",
			MountPath: "/ipc",
		},
		{
			Name:      "cardano-config",
			MountPath: "/configuration",
		},
	}

	if coreNode {
		cardanoNode.VolumeMounts = append(cardanoNode.VolumeMounts, corev1.VolumeMount{Name: "nodeop-secrets", MountPath: "/nodeop"})
	}

	state.Spec.Template.Spec.Containers = append(state.Spec.Template.Spec.Containers, cardanoNode)

	// add container volumes like node-ipc and cardano-config
	state.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "node-ipc",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: nil,
			},
		},
		{
			Name: "cardano-config",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ConfigMap: &corev1.ConfigMapProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: fmt.Sprintf("%s-config", name),
								},
							},
						},
						{
							ConfigMap: &corev1.ConfigMapProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: fmt.Sprintf("%s-topology", name),
								},
							},
						},
					},
				},
			},
		},
	}

	if coreNode {
		state.Spec.Template.Spec.Volumes = append(state.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "nodeop-secrets",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "hot.skey",
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "vrf.skey",
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "op.cert",
								},
							},
						},
					},
				},
			},
		})
	}

	// add volumeClaimTemplate
	state.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-db",
			},
			Spec: storage,
		},
	}

	return state
}

func generateNodeService(name string,
	namespace string,
	annotations map[string]string,
	labels map[string]string,
	service cardanov1.NodeServiceSpec) *corev1.Service {

	svc := &corev1.Service{}

	svc.ObjectMeta = metav1.ObjectMeta{
		Name:        name,
		Namespace:   namespace,
		Annotations: annotations,
	}

	svc.Spec.Selector = labels

	if service.Type != corev1.ServiceTypeLoadBalancer && service.Type != corev1.ServiceTypeNodePort {
		svc.Spec.ClusterIP = "None"
	}

	svc.Spec.Type = service.Type

	svc.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "cardano",
			Port:       service.Port,
			TargetPort: intstr.FromInt(31400),
			Protocol:   corev1.ProtocolTCP,
		},
	}

	return svc
}

func ensureSpec(replicas int32, found *appsv1.StatefulSet, image string, r client.Client) (ctrl.Result, error) {

	ctx := context.Background()

	// Ensure the statefulset size is the same as the spec
	if *found.Spec.Replicas != replicas {
		*found.Spec.Replicas = replicas
		err := r.Update(ctx, found)
		if err != nil {
			return ctrl.Result{}, err
		}
		// Spec updated - return and requeue
		return ctrl.Result{Requeue: true}, nil
	}

	// Ensure the statefulset image is the same as the spec
	for key, container := range found.Spec.Template.Spec.Containers {
		if strings.EqualFold(container.Name, "cardano-node") && container.Image != image {
			found.Spec.Template.Spec.Containers[key].Image = image
			err := r.Update(ctx, found)
			if err != nil {

				return ctrl.Result{}, err
			}
			// Spec updated - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, nil
}

func updateStatus(name string, namespace string, nodes []string, r client.Client, obj runtime.Object) (ctrl.Result, error) {

	ctx := context.Background()

	// Update the Relay status with the pod names
	// List the pods for this relay's statefulset
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labelsForRelay(name)),
	}
	if err := r.List(ctx, podList, listOpts...); err != nil {
		return ctrl.Result{}, err
	}
	podNames := getPodNames(podList.Items)

	// Update status.Nodes if needed
	if !reflect.DeepEqual(podNames, nodes) {
		nodes = podNames
		err := r.Status().Update(ctx, obj)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

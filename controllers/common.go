package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	cardanov1 "github.com/zenithpool/cardano-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	serviceModeAnnotation = "cardano.io/mode"
	podDesignationLabel   = "cardano.io/designation"
)

func generateNodeStatefulset(name string,
	namespace string,
	labels map[string]string,
	nodeSpec cardanov1.NodeSpec,
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

	state.Spec.Replicas = &nodeSpec.Replicas

	state.Spec.Template.Spec.ImagePullSecrets = nodeSpec.ImagePullSecrets

	state.Spec.Template.ObjectMeta.Labels = labels
	state.Spec.Template.ObjectMeta.Annotations = map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/path":   "/metrics",
		"prometheus.io/port":   "8080",
	}

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

	// Create pod container details
	cardanoNode := corev1.Container{}

	cardanoNode.Name = "cardano-node"
	cardanoNode.Image = nodeSpec.Image
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

	cardanoNode.Resources = nodeSpec.Resources

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

	// the inputoutput images need to have their genesis files moved to a generalized filepath
	// create InitContainers to move genesis files to /genesis
	if strings.HasPrefix(nodeSpec.Image, "inputoutput/cardano") {
		cardanoNode.VolumeMounts = append(cardanoNode.VolumeMounts, corev1.VolumeMount{Name: "genesis", MountPath: "/genesis"})
		state.Spec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name:  "cardano-node-init",
				Image: nodeSpec.Image,
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "genesis",
						MountPath: "/genesis",
					},
				},
				Command: []string{"sh", "-c", "cp /nix/store/*-mainnet-byron-genesis.json /genesis/byron-genesis.json && cp /nix/store/*-mainnet-shelley-genesis.json /genesis/shelley-genesis.json"},
			},
		}
		state.Spec.Template.Spec.Volumes = append(state.Spec.Template.Spec.Volumes, corev1.Volume{Name: "genesis", VolumeSource: corev1.VolumeSource{EmptyDir: nil}})
	}

	state.Spec.Template.Spec.Containers = append(state.Spec.Template.Spec.Containers, cardanoNode)

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
			Spec: nodeSpec.Storage,
		},
	}

	return state
}

func generateNodeService(name string,
	namespace string,
	labels map[string]string,
	service cardanov1.NodeServiceSpec) *corev1.Service {

	svc := &corev1.Service{}

	svc.ObjectMeta = metav1.ObjectMeta{
		Name:        name,
		Namespace:   namespace,
		Annotations: service.Annotations,
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

func updateStatus(name string, namespace string, labels map[string]string, nodes []string, r client.Client, fn func([]string) (ctrl.Result, error)) (ctrl.Result, error) {

	ctx := context.Background()

	// Update the Relay status with the pod names
	// List the pods for this relay's statefulset
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labels),
	}
	if err := r.List(ctx, podList, listOpts...); err != nil {
		return ctrl.Result{}, err
	}
	podNames := getPodNames(podList.Items)

	// Update status.Nodes if needed
	if !reflect.DeepEqual(podNames, nodes) {

		return fn(podNames)
	}

	return ctrl.Result{}, nil
}

func ensureActiveStandby(name string, namespace string, labels map[string]string, r client.Client) (ctrl.Result, error) {
	ctx := context.Background()

	// get all svc's selectors
	svc := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, svc)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("Failed to get service: %s", err.Error())
	}

	// get eligible pods
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labels),
	}
	if err := r.List(ctx, podList, listOpts...); err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	// filter by runnning pods
	eligiblePods := []corev1.Pod{}
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			ready := true
			for _, container := range pod.Status.ContainerStatuses {
				if !container.Ready {
					ready = false
					break
				}
			}

			// if pod's containers are not ready, pod is not ready
			if !ready {
				continue
			}

			// TODO check if cardano container is sync

			// pod is ready and all containers are ready, add to eligibleList
			eligiblePods = append(eligiblePods, pod)
		}
	}

	// if there is not an active pod, promote one
	foundPodDesignated := false
	for _, pod := range eligiblePods {
		if _, found := pod.Labels[podDesignationLabel]; found {
			foundPodDesignated = true
			break
		}
	}
	if !foundPodDesignated && len(eligiblePods) > 0 {
		pod := eligiblePods[0]
		patch := client.MergeFrom(pod.DeepCopy())
		pod.Labels[podDesignationLabel] = "true"
		err = r.Patch(ctx, &pod, patch)
		if err != nil {
			return ctrl.Result{Requeue: true}, fmt.Errorf("Unable to patch pod with designation label: %s", err.Error())
		}
	}

	// pod designation label to selector to svc
	if _, found := svc.Spec.Selector[podDesignationLabel]; !found {
		patch := client.MergeFrom(svc.DeepCopy())
		svc.Spec.Selector[podDesignationLabel] = "true"
		err = r.Patch(ctx, svc, patch)
		if err != nil {
			return ctrl.Result{Requeue: true}, fmt.Errorf("Unable to patch svc with designation label: %s", err.Error())
		}
	}

	return ctrl.Result{}, nil
}

func createConfigMap(name string, namespace string, key string, data string, r client.Client, obj v1.Object, scheme *runtime.Scheme) (ctrl.Result, error) {

	ctx := context.Background()

	// Check if configMaps already exists, if not create a new one
	configMap := &corev1.ConfigMap{}
	configKey := types.NamespacedName{Name: name, Namespace: namespace}
	err := r.Get(ctx, configKey, configMap)
	if err != nil && errors.IsNotFound(err) {
		// Define a new configMap
		configMap = &corev1.ConfigMap{
			ObjectMeta: v1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Data: map[string]string{key: data},
		}
		ctrl.SetControllerReference(obj, configMap, scheme)

		err = r.Create(ctx, configMap)
		if err != nil {
			return ctrl.Result{}, err
		}
		// ConfigMap created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// TODO check if data is same, if not then update

	return ctrl.Result{}, nil
}

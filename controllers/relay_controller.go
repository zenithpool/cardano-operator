/*


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

package controllers

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cardanov1 "github.com/zenithpool/cardano-operator/api/v1"
)

// RelayReconciler reconciles a Relay object
type RelayReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cardano.zenithpool.io,resources=relays,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cardano.zenithpool.io,resources=relays/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;patch;delete

func (r *RelayReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("relay", req.NamespacedName)

	// your logic here

	// Fetch the Relay instance
	relay := &cardanov1.Relay{}
	err := r.Get(ctx, req.NamespacedName, relay)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Info("Relay resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get Relay")
		return ctrl.Result{}, err
	}

	// Check if the statefulset already exists, if not create a new one
	found := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: relay.Name, Namespace: relay.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		// Define a new statefulset
		dep := r.statefulsetForRelay(relay)
		log.Info("Creating a new Statefuleset", "StatefulSet.Namespace", dep.Namespace, "StatefulSet.Name", dep.Name)
		err = r.Create(ctx, dep)
		if err != nil {
			log.Error(err, "Failed to create new StatefulSet", "StatefulSet.Namespace", dep.Namespace, "StatefulSet.Name", dep.Name)
			return ctrl.Result{}, err
		}
		// StatefulSet created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		log.Error(err, "Failed to get StatefulSet")
		return ctrl.Result{}, err
	}

	// Ensure the statefulset size is the same as the spec
	size := relay.Spec.Spec.Replicas
	if size != nil && *found.Spec.Replicas != *size {
		found.Spec.Replicas = size
		err = r.Update(ctx, found)
		if err != nil {
			log.Error(err, "Failed to update StatefulSet", "StatefulSet.Namespace", found.Namespace, "StatefulSet.Name", found.Name)
			return ctrl.Result{}, err
		}
		// Spec updated - return and requeue
		return ctrl.Result{Requeue: true}, nil
	}

	// Update the Relay status with the pod names
	// List the pods for this relay's statefulset
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(relay.Namespace),
		client.MatchingLabels(labelsForRelay(relay.Name)),
	}
	if err = r.List(ctx, podList, listOpts...); err != nil {
		log.Error(err, "Failed to list pods", "Relay.Namespace", relay.Namespace, "Relay.Name", relay.Name)
		return ctrl.Result{}, err
	}
	podNames := getPodNames(podList.Items)

	// Update status.Nodes if needed
	if !reflect.DeepEqual(podNames, relay.Status.Nodes) {
		relay.Status.Nodes = podNames
		err := r.Status().Update(ctx, relay)
		if err != nil {
			log.Error(err, "Failed to update Relay status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// statefulsetForRelay returns a memcached StatefulSet object
func (r *RelayReconciler) statefulsetForRelay(relay *cardanov1.Relay) *appsv1.StatefulSet {
	ls := labelsForRelay(relay.Name)

	state := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      relay.Name,
			Namespace: relay.Namespace,
		},
		Spec: *relay.Spec.Spec,
	}

	if state.Spec.ServiceName == "" {
		state.Spec.ServiceName = relay.Name
	}

	state.Spec.Selector.MatchLabels = ls

	state.Spec.Template.ObjectMeta.Labels = ls
	state.Spec.Template.ObjectMeta.Annotations = map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/path":   "/metrics",
		"prometheus.io/port":   "8080",
	}

	// Set Relay instance as the owner and controller
	ctrl.SetControllerReference(relay, state, r.Scheme)
	return state
}

// labelsForRelay returns the labels for selecting the resources
// belonging to the given memcached CR name.
func labelsForRelay(name string) map[string]string {
	return map[string]string{"app": "cardano-node", "relay_cr": name}
}

// getPodNames returns the pod names of the array of pods passed in
func getPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}

func (r *RelayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cardanov1.Relay{}).
		Complete(r)
}

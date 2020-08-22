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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

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

	// Check if the service already exists, if not create a new one
	foundSvc := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: relay.Name, Namespace: relay.Namespace}, foundSvc)
	if err != nil && errors.IsNotFound(err) {
		// Define a new service
		svc := r.serviceForRelay(relay)
		log.Info("Creating a new Service", "Service.Namespace", svc.Namespace, "Service.Name", svc.Name)
		err = r.Create(ctx, svc)
		if err != nil {
			log.Error(err, "Failed to create new Service", "Service.Namespace", svc.Namespace, "Service.Name", svc.Name)
			return ctrl.Result{}, err
		}
		// Service created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Service")
		return ctrl.Result{}, err
	}

	result, err := ensureSpec(relay.Spec.Replicas, found, relay.Spec.Image, r)
	if err != nil || result.Requeue {
		if err != nil {
			log.Error(err, "Failed to update StatefulSet", "StatefulSet.Namespace", found.Namespace, "StatefulSet.Name", found.Name)
		}
		return result, err
	}

	result, err = updateStatus(relay.Name, relay.Namespace, labelsForRelay(relay.Name), relay.Status.Nodes, r.Client, func(pods []string) (ctrl.Result, error) {
		relay.Status.Nodes = pods
		err := r.Status().Update(ctx, relay)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	})
	if err != nil || result.Requeue {
		if err != nil {
			log.Error(err, "Failed to update status", "Relay.Namespace", found.Namespace, "Relay.Name", found.Name)
		}
		return result, err
	}

	return ctrl.Result{}, nil
}

// serviceForRelay returns a Relay Service object
func (r *RelayReconciler) serviceForRelay(relay *cardanov1.Relay) *corev1.Service {
	ls := labelsForRelay(relay.Name)

	svc := generateNodeService(relay.Name, relay.Namespace, ls, relay.Spec.Service)

	// Set Relay instance as the owner and controller
	ctrl.SetControllerReference(relay, svc, r.Scheme)
	return svc
}

// statefulsetForRelay returns a Relay StatefulSet object
func (r *RelayReconciler) statefulsetForRelay(relay *cardanov1.Relay) *appsv1.StatefulSet {
	ls := labelsForRelay(relay.Name)

	state := generateNodeStatefulset(relay.Name,
		relay.Namespace,
		ls,
		relay.Spec.NodeSpec,
		false,
	)

	// Set Relay instance as the owner and controller
	ctrl.SetControllerReference(relay, state, r.Scheme)
	return state
}

// labelsForRelay returns the labels for selecting the resources
// belonging to the given memcached CR name.
func labelsForRelay(name string) map[string]string {
	return map[string]string{
		"app":      "cardano-node",
		"relay_cr": name,
		"instance": "relay",
	}
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

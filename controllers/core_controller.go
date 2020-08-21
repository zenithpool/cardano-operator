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

// CoreReconciler reconciles a Core object
type CoreReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cardano.zenithpool.io,resources=cores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cardano.zenithpool.io,resources=cores/status,verbs=get;update;patch

func (r *CoreReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("core", req.NamespacedName)

	// your logic here

	// Fetch the Core instance
	core := &cardanov1.Core{}
	err := r.Get(ctx, req.NamespacedName, core)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Info("Core resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get Core")
		return ctrl.Result{}, err
	}

	// Check if the statefulset already exists, if not create a new one
	found := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: core.Name, Namespace: core.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		// Define a new statefulset
		dep := r.statefulsetForCore(core)
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
	err = r.Get(ctx, types.NamespacedName{Name: core.Name, Namespace: core.Namespace}, foundSvc)
	if err != nil && errors.IsNotFound(err) {
		// Define a new service
		svc := r.serviceForCore(core)
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

	result, err := ensureSpec(core.Spec.Replicas, found, core.Spec.Image, r)
	if err != nil || result.Requeue {
		if err != nil {
			log.Error(err, "Failed to update StatefulSet", "StatefulSet.Namespace", found.Namespace, "StatefulSet.Name", found.Name)
		}
		return result, err
	}

	result, err = updateStatus(core.Name, core.Namespace, core.Status.Nodes, r.Client, core)
	if err != nil || result.Requeue {
		if err != nil {
			log.Error(err, "Failed to update status", "Core.Namespace", found.Namespace, "Core.Name", found.Name)
		}
		return result, err
	}

	return ctrl.Result{}, nil
}

// serviceForCore returns a Relay Service object
func (r *CoreReconciler) serviceForCore(core *cardanov1.Core) *corev1.Service {
	ls := labelsForCore(core.Name)

	svc := generateNodeService(core.Name, core.Namespace, core.Annotations, ls, core.Spec.Service)

	// Set Core instance as the owner and controller
	ctrl.SetControllerReference(core, svc, r.Scheme)
	return svc
}

func (r *CoreReconciler) statefulsetForCore(core *cardanov1.Core) *appsv1.StatefulSet {
	ls := labelsForCore(core.Name)

	state := generateNodeStatefulset(core.Name,
		core.Namespace,
		core.Spec.Image,
		ls,
		core.Spec.Replicas,
		core.Spec.ImagePullSecrets,
		core.Spec.Storage,
		true,
	)

	// Set Relay instance as the owner and controller
	ctrl.SetControllerReference(core, state, r.Scheme)
	return state
}

// labelsForCore returns the labels for selecting the resources
// belonging to the given memcached CR name.
func labelsForCore(name string) map[string]string {
	return map[string]string{
		"app":      "cardano-node",
		"relay_cr": name,
		"instance": "core",
	}
}

func (r *CoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cardanov1.Core{}).
		Complete(r)
}

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
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1beta1"

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
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;create;update;patch;delete

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

	// Check if the PodDisruptionBudget already exists, if not create a new one
	foundPDB := &policyv1.PodDisruptionBudget{}
	err = r.Get(ctx, types.NamespacedName{Name: core.Name, Namespace: core.Namespace}, foundPDB)
	if err != nil && errors.IsNotFound(err) {
		// Define a new PodDisruptionPolicy
		pdb := r.pdbForCore(core)
		if pdb != nil {
			log.Info("Creating a new PodDisruptionPolicy", "PodDisruptionPolicy.Namespace", pdb.Namespace, "PodDisruptionPolicy.Name", pdb.Name)
			err = r.Create(ctx, pdb)
			if err != nil {
				log.Error(err, "Failed to create new PodDisruptionPolicy", "PodDisruptionPolicy.Namespace", pdb.Namespace, "PodDisruptionPolicy.Name", pdb.Name)
				return ctrl.Result{}, err
			}

			return ctrl.Result{Requeue: true}, nil
		}
	} else if err != nil {
		log.Error(err, "Failed to get PodDisruptionPolicy")
		return ctrl.Result{}, err
	}

	// Check if PodDisruptionBudget should be removed
	if minav, err := intstr.GetValueFromIntOrPercent(foundPDB.Spec.MinAvailable, 1, false); err == nil {
		if int32(minav) >= core.Spec.Replicas {
			// Need to delete PDB
			r.Delete(ctx, foundPDB)
		}
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

	result, err := ensureSpec(core.Spec.Replicas, found, core.Spec.NodeSpec, r)
	if err != nil || result.Requeue {
		if err != nil {
			log.Error(err, "Failed to update StatefulSet", "StatefulSet.Namespace", found.Namespace, "StatefulSet.Name", found.Name)
		}
		return result, err
	}

	result, err = updateStatus(core.Name, core.Namespace, labelsForCore(core.Name), core.Status.Nodes, r.Client, func(pods []string) (ctrl.Result, error) {
		core.Status.Nodes = pods
		err := r.Status().Update(ctx, core)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	})
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

	svc := generateNodeService(core.Name, core.Namespace, ls, core.Spec.Service)

	// Set Core instance as the owner and controller
	ctrl.SetControllerReference(core, svc, r.Scheme)
	return svc
}

func (r *CoreReconciler) statefulsetForCore(core *cardanov1.Core) *appsv1.StatefulSet {
	ls := labelsForCore(core.Name)

	state := generateNodeStatefulset(core.Name,
		core.Namespace,
		ls,
		core.Spec.NodeSpec,
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

func (r *CoreReconciler) ActiveStandbyWatch() {
	ctx := context.Background()
	log := r.Log

	// your logic here

	for {
		time.Sleep(1 * time.Second)

		// get list of core
		coreList := &cardanov1.CoreList{}
		err := r.List(ctx, coreList)
		if err != nil {
			log.Error(err, "Unable to get core list")
			continue
		}

		for _, core := range coreList.Items {
			result, err := ensureActiveStandby(core.Name, core.Namespace, labelsForCore(core.Name), r.Client)
			if err != nil || result.Requeue {
				if err != nil {
					log.Error(err, "Failed to ensure active/standby", "Core.Namespace", core.Namespace, "Core.Name", core.Name)
				}
				continue
			}
		}

	}

}

func (r *CoreReconciler) pdbForCore(core *cardanov1.Core) *policyv1.PodDisruptionBudget {
	ls := labelsForCore(core.Name)

	if core.Spec.Replicas <= 1 {
		return nil

	}

	minAvailable := intstr.FromInt(1)

	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      core.Name,
			Namespace: core.Namespace,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
		},
	}

	// Set Relay instance as the owner and controller
	ctrl.SetControllerReference(core, pdb, r.Scheme)
	return pdb
}

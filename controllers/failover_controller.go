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
	cardanov1 "github.com/zenithpool/cardano-operator/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FailOverReconciler reconciles a FailOver object
type FailOverReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cardano.zenithpool.io,resources=failovers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cardano.zenithpool.io,resources=failovers/status,verbs=get;update;patch

func (r *FailOverReconciler) Reconcile() {
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

/*
Copyright 2021.

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

package v1beta1

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sambatv/spaces-operator/apis/v1beta1"
	"github.com/sambatv/spaces-operator/kube"
)

// TeamReconciler reconciles a Team object.
type TeamReconciler struct {
	ctrlclient.Client
	Log    logr.Logger
	Scheme *apiruntime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=namespaces;namespaces/finalize;namespaces/status,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=core,resources=nodes;nodes/proxy;nodes/status;persistentvolumes;persistentvolumes/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=bindings;configmaps;endpoints;events;persistentvolumeclaims;persistentvolumeclaims/status;pods;pods/attach;pods/binding;pods/eviction;pods/exec;pods/log;pods/portforward;pods/proxy;pods/status;replicasets;replicationcontrollers;replicationcontrollers/scale;replicationcontrollers/status;resourcequotas;resourcequotas/status;secrets;serviceaccounts;serviceaccounts/token;services;services/proxy;services/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions;customresourcedefinitions/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=controllerrevisions;daemonsets;daemonsets/status;deployments;deployments/scale;deployments/status;replicasets;replicasets/scale;replicasets/status;statefulsets;statefulsets/scale;statefulsets/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers;horizontalpodautoscalers/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs;cronjobs/status;jobs;jobs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses;ingresses;ingresses/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=spaces.samba.tv,resources=teams,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=spaces.samba.tv,resources=teams/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=spaces.samba.tv,resources=teams/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *TeamReconciler) Reconcile(ctx context.Context, req ctrlruntime.Request) (ctrlruntime.Result, error) {
	teamName := req.NamespacedName.Name

	log := r.Log.WithValues("team", teamName)
	log.Info("reconciling...")

	// Get a kubernetes client configured for in-cluster use.
	kubeClient, err := kube.GetClient()
	if err != nil {
		log.Error(err, "failure getting kubernetes client")
		return ctrlruntime.Result{}, err
	}

	//  Try getting the Team by name.
	var team v1beta1.Team
	if err := r.Get(ctx, req.NamespacedName, &team); err != nil {
		// If any error other than a "NotFound" StatusError, it's a problem.
		statusErr, ok := err.(*apierrors.StatusError)
		if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
			err = ErrTeamResourceNotFound{
				TeamName:     teamName,
				ResourceKind: "Team",
				ResourceName: teamName,
			}
			log.Error(err, "error fetching team")
			return ctrlruntime.Result{}, err
		}

		// Success reconciling Team deletion!
		return ctrlruntime.Result{}, nil
	}
	log.Info("fetched team")

	// Ensure namespaces for Team resources exists.
	for _, namespaceName := range team.Spec.Namespaces {
		// Ensure namespace for Team resources exists.
		teamNamespace, err := kubeClient.CoreV1().Namespaces().Get(ctx, namespaceName, apimetav1.GetOptions{})
		if err != nil {
			statusErr, ok := err.(*apierrors.StatusError)
			if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
				log.Error(err, "failure fetching team namespace")
				return ctrlruntime.Result{}, err
			}
			// Create team namespace.
			teamNamespace = &corev1.Namespace{
				ObjectMeta: apimetav1.ObjectMeta{
					Name:   namespaceName,
					Labels: controllerLabels,
				},
			}
			teamNamespace, err = kubeClient.CoreV1().Namespaces().Create(ctx, teamNamespace, apimetav1.CreateOptions{})
			if err != nil {
				log.Error(err, "failure creating team namespace")
				return ctrlruntime.Result{}, err
			}
			log.Info("created team namespace", "name", teamNamespace.Name)
		} else {
			log.Info("fetched team namespace", "name", teamNamespace.Name)
		}

		// Ensure team rolebindings for each clusterrole exist.
		for _, clusterRoleName := range team.Spec.ClusterRoles {
			// Check if it exists.
			_, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, clusterRoleName, apimetav1.GetOptions{})
			if err != nil {
				statusErr, ok := err.(*apierrors.StatusError)
				if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
					log.Error(err, "failure fetching clusterrole", "clusterrole", clusterRoleName)
					return ctrlruntime.Result{}, err
				}
			}

			// Create rolebinding when not found.
			roleBindingName := fmt.Sprintf("%s-team-%s", teamName, clusterRoleName)
			roleBinding, err := kubeClient.RbacV1().RoleBindings(namespaceName).Get(ctx, roleBindingName, apimetav1.GetOptions{})
			if err != nil {
				statusErr, ok := err.(*apierrors.StatusError)
				if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
					log.Error(err, "failure fetching team rolebinding", "name", roleBindingName, "namespace", namespaceName)
					return ctrlruntime.Result{}, err
				}

				roleBinding = &rbacv1.RoleBinding{
					ObjectMeta: apimetav1.ObjectMeta{
						Name:      roleBindingName,
						Namespace: namespaceName,
						Labels:    controllerLabels,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "User",
							APIGroup:  "rbac.authorization.k8s.io",
							Name:      team.Name,
							Namespace: namespaceName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: RbacAPIGroup,
						Kind:     "ClusterRole",
						Name:     clusterRoleName,
					},
				}
				err := ctrlruntime.SetControllerReference(&team, roleBinding, r.Scheme)
				if err != nil {
					log.Error(err, "failure setting controller reference", "team", teamName, "rolebinding", roleBindingName, "namespace", namespaceName)
					return ctrlruntime.Result{}, err
				}
				err = r.Create(ctx, roleBinding)
				if err != nil {
					log.Error(err, "failure creating rolebinding", "team", teamName, "rolebinding", roleBindingName, "namespace", namespaceName)
					return ctrlruntime.Result{}, err
				}

				log.Info("created team rolebinding", "name", roleBinding.Name, "namespace", roleBinding.Namespace)
				continue
			}

			// Update it when found.
			roleBinding = &rbacv1.RoleBinding{
				ObjectMeta: apimetav1.ObjectMeta{
					Name:      roleBindingName,
					Namespace: teamNamespace.Name,
					Labels:    controllerLabels,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "User",
						APIGroup:  "rbac.authorization.k8s.io",
						Name:      team.Name,
						Namespace: teamNamespace.Name,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: RbacAPIGroup,
					Kind:     "ClusterRole",
					Name:     clusterRoleName,
				},
			}
			_, err = kubeClient.RbacV1().RoleBindings(teamNamespace.Name).Update(ctx, roleBinding, apimetav1.UpdateOptions{})
			if err != nil {
				log.Error(err, "failure updating team rolebinding", "name", roleBindingName, "namespace", teamNamespace.Name)
				return ctrlruntime.Result{}, err
			}
		}
	}

	// Success!
	return ctrlruntime.Result{}, nil
}

// SetupWithManager sets up the controller with the Mapper.
func (r *TeamReconciler) SetupWithManager(mgr ctrlruntime.Manager) error {
	return ctrlruntime.NewControllerManagedBy(mgr).
		For(&v1beta1.Team{}).
		Complete(r)
}

// ErrTeamResourceNotFound indicates a Team resource was not found.
type ErrTeamResourceNotFound struct {
	TeamName          string
	ResourceKind      string
	ResourceName      string
	ResourceNamespace string
}

func (e ErrTeamResourceNotFound) Error() string {
	if e.ResourceNamespace != "" {
		return fmt.Sprintf("%s team resource %s of kind %s not found in %s namespace",
			e.TeamName, e.ResourceName, e.ResourceKind, e.ResourceNamespace)
	}
	return fmt.Sprintf("%s team resource %s of kind %s not found in cluster",
		e.TeamName, e.ResourceName, e.ResourceKind)
}

const RbacAPIGroup = "rbac.authorization.k8s.io"

// controllerLabels are the labels added to all objects created by Team controller.
var controllerLabels = map[string]string{
	"app.kubernetes.io/created-by": "spaces-operator.samba.tv",
}

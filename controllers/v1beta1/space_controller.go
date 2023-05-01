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
	"k8s.io/apimachinery/pkg/runtime"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sambatv/spaces-operator/apis/v1beta1"
	"github.com/sambatv/spaces-operator/kube"
)

// SpaceReconciler reconciles a Space object
type SpaceReconciler struct {
	ctrlclient.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=namespaces;namespaces/finalize;namespaces/status,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=core,resources=nodes;nodes/proxy;nodes/status;persistentvolumes;persistentvolumes/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=bindings;configmaps;endpoints;events;persistentvolumeclaims;persistentvolumeclaims/status;pods;pods/attach;pods/binding;pods/eviction;pods/exec;pods/log;pods/portforward;pods/proxy;pods/status;replicasets;replicationcontrollers;replicationcontrollers/scale;replicationcontrollers/status;resourcequotas;resourcequotas/status;secrets;serviceaccounts;serviceaccounts/token;services;services/proxy;services/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions;customresourcedefinitions/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=controllerrevisions;daemonsets;daemonsets/status;deployments;deployments/scale;deployments/status;replicasets;replicasets/scale;replicasets/status;statefulsets;statefulsets/scale;statefulsets/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers;horizontalpodautoscalers/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs;cronjobs/status;jobs;jobs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses;ingresses;ingresses/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=spaces.samba.tv,resources=spaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=spaces.samba.tv,resources=spaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=spaces.samba.tv,resources=spaces/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *SpaceReconciler) Reconcile(ctx context.Context, req ctrlruntime.Request) (ctrlruntime.Result, error) {
	spaceName := req.NamespacedName.Name

	log := r.Log.WithValues("space", spaceName)
	log.Info("reconciling Space controller...")

	// Get a kubernetes client configured for in-cluster use.
	kubeClient, err := kube.GetClient()
	if err != nil {
		log.Error(err, "failure getting kubernetes client")
		return ctrlruntime.Result{}, err
	}

	//  Try getting the Space by name.
	var space v1beta1.Space
	if err := r.Get(ctx, req.NamespacedName, &space); err != nil {
		// If any error other than a "NotFound" StatusError, it's a problem.
		statusErr, ok := err.(*apierrors.StatusError)
		if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
			err = ErrSpaceResourceNotFound{
				SpaceName:    spaceName,
				ResourceKind: "Space",
				ResourceName: spaceName,
			}
			log.Error(err, "error fetching Space resource")
			return ctrlruntime.Result{}, err
		}

		// Success reconciling Space deletion!
		log.Info("deleted Space resource")
		return ctrlruntime.Result{}, nil
	}
	log.Info("fetched Space resource")

	// Ensure namespaces for Space-owned resources exists.
	for _, namespaceName := range space.Spec.Namespaces {
		spaceNamespace, err := kubeClient.CoreV1().Namespaces().Get(ctx, namespaceName, apimetav1.GetOptions{})
		if err != nil {
			statusErr, ok := err.(*apierrors.StatusError)
			if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
				log.Error(err, "failure fetching space namespace")
				return ctrlruntime.Result{}, err
			}

			spaceNamespace = &corev1.Namespace{
				ObjectMeta: apimetav1.ObjectMeta{
					Name:   namespaceName,
					Labels: controllerLabels,
				},
			}
			spaceNamespace, err = kubeClient.CoreV1().Namespaces().Create(ctx, spaceNamespace, apimetav1.CreateOptions{})
			if err != nil {
				log.Error(err, "failure creating Space namespace")
				return ctrlruntime.Result{}, err
			}
			log.Info("created Space namespace", "namespace", namespaceName)
		} else {
			log.Info("fetched Space namespace", "namespace", namespaceName)
		}

		// All Space-owned RoleBindings use the same normalized prefix.
		resourcePrefix := strings.TrimSuffix(spaceName, "-space")
		resourcePrefix = strings.TrimSuffix(spaceName, ".space")
		resourcePrefix = fmt.Sprintf("%s-space", resourcePrefix)

		// Ensure Space rolebindings for each clusterrole exist.
		for _, clusterRoleName := range space.Spec.ClusterRoles {
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
			roleBindingName := fmt.Sprintf("%s-clusterrole-%s", resourcePrefix, clusterRoleName)
			roleBinding, err := kubeClient.RbacV1().RoleBindings(namespaceName).Get(ctx, roleBindingName, apimetav1.GetOptions{})
			if err != nil {
				statusErr, ok := err.(*apierrors.StatusError)
				if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
					log.Error(err, "failure fetching Space rolebinding", "rolebinding", roleBindingName, "namespace", namespaceName, "clusterrole", clusterRoleName)
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
							Name:      space.Name,
							Namespace: namespaceName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: RbacAPIGroup,
						Kind:     "ClusterRole",
						Name:     clusterRoleName,
					},
				}
				err := ctrlruntime.SetControllerReference(&space, roleBinding, r.Scheme)
				if err != nil {
					log.Error(err, "failure setting Space controller reference", "rolebinding", roleBindingName, "namespace", namespaceName, "clusterrole", clusterRoleName)
					return ctrlruntime.Result{}, err
				}
				err = r.Create(ctx, roleBinding)
				if err != nil {
					log.Error(err, "failure creating Space rolebinding", "rolebinding", roleBindingName, "namespace", namespaceName, "clusterrole", clusterRoleName)
					return ctrlruntime.Result{}, err
				}

				log.Info("created Space rolebinding", "rolebinding", roleBinding.Name, "namespace", roleBinding.Namespace, "clusterrole", clusterRoleName)
				continue
			}

			// Update it when found.
			roleBinding = &rbacv1.RoleBinding{
				ObjectMeta: apimetav1.ObjectMeta{
					Name:      roleBindingName,
					Namespace: spaceNamespace.Name,
					Labels:    controllerLabels,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "User",
						APIGroup:  "rbac.authorization.k8s.io",
						Name:      space.Name,
						Namespace: spaceNamespace.Name,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: RbacAPIGroup,
					Kind:     "ClusterRole",
					Name:     clusterRoleName,
				},
			}
			_, err = kubeClient.RbacV1().RoleBindings(spaceNamespace.Name).Update(ctx, roleBinding, apimetav1.UpdateOptions{})
			if err != nil {
				log.Error(err, "failure updating Space rolebinding", "rolebinding", roleBindingName, "namespace", spaceNamespace.Name, "clusterrole", clusterRoleName)
				return ctrlruntime.Result{}, err
			}
		}

		// Ensure Space rolebindings for each clusterrole exist.
		for _, roleName := range space.Spec.Roles {
			// Check if it exists.
			_, err := kubeClient.RbacV1().Roles(namespaceName).Get(ctx, roleName, apimetav1.GetOptions{})
			if err != nil {
				statusErr, ok := err.(*apierrors.StatusError)
				if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
					log.Error(err, "failure fetching role", "role", roleName)
					return ctrlruntime.Result{}, err
				}
			}

			// Create rolebinding when not found.
			roleBindingName := fmt.Sprintf("%s-role-%s", resourcePrefix, roleName)
			roleBinding, err := kubeClient.RbacV1().RoleBindings(namespaceName).Get(ctx, roleBindingName, apimetav1.GetOptions{})
			if err != nil {
				statusErr, ok := err.(*apierrors.StatusError)
				if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
					log.Error(err, "failure fetching Space rolebinding", "rolebinding", roleBindingName, "namespace", namespaceName, "role", roleName)
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
							Name:      space.Name,
							Namespace: namespaceName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: RbacAPIGroup,
						Kind:     "Role",
						Name:     roleName,
					},
				}
				err := ctrlruntime.SetControllerReference(&space, roleBinding, r.Scheme)
				if err != nil {
					log.Error(err, "failure setting Space controller reference", "rolebinding", roleBindingName, "namespace", namespaceName, "role", roleName)
					return ctrlruntime.Result{}, err
				}
				err = r.Create(ctx, roleBinding)
				if err != nil {
					log.Error(err, "failure creating Space rolebinding", "rolebinding", roleBindingName, "namespace", namespaceName, "role", roleName)
					return ctrlruntime.Result{}, err
				}

				log.Info("created Space rolebinding", "rolebinding", roleBinding.Name, "namespace", roleBinding.Namespace, "role", roleName)
				continue
			}

			// Update it when found.
			roleBinding = &rbacv1.RoleBinding{
				ObjectMeta: apimetav1.ObjectMeta{
					Name:      roleBindingName,
					Namespace: spaceNamespace.Name,
					Labels:    controllerLabels,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "User",
						APIGroup:  "rbac.authorization.k8s.io",
						Name:      space.Name,
						Namespace: spaceNamespace.Name,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: RbacAPIGroup,
					Kind:     "Role",
					Name:     roleName,
				},
			}
			_, err = kubeClient.RbacV1().RoleBindings(spaceNamespace.Name).Update(ctx, roleBinding, apimetav1.UpdateOptions{})
			if err != nil {
				log.Error(err, "failure updating Space rolebinding", "rolebinding", roleBindingName, "namespace", spaceNamespace.Name, "role", roleName)
				return ctrlruntime.Result{}, err
			}
		}
	}

	// Success!
	return ctrlruntime.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SpaceReconciler) SetupWithManager(mgr ctrlruntime.Manager) error {
	return ctrlruntime.NewControllerManagedBy(mgr).
		For(&v1beta1.Space{}).
		Complete(r)
}

// ErrSpaceResourceNotFound indicates a Space resource was not found.
type ErrSpaceResourceNotFound struct {
	SpaceName         string
	ResourceKind      string
	ResourceName      string
	ResourceNamespace string
}

func (e ErrSpaceResourceNotFound) Error() string {
	if e.ResourceNamespace != "" {
		return fmt.Sprintf("%s space resource %s of kind %s not found in %s namespace",
			e.SpaceName, e.ResourceName, e.ResourceKind, e.ResourceNamespace)
	}
	return fmt.Sprintf("%s space resource %s of kind %s not found in cluster",
		e.SpaceName, e.ResourceName, e.ResourceKind)
}

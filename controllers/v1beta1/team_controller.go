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

//+kubebuilder:rbac:groups=core,resources=namespaces;namespaces/finalize;namespaces/status,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=core,resources=nodes;nodes/proxy;nodes/status;persistentvolumes;persistentvolumes/status,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=bindings;configmaps;endpoints;events;persistentvolumeclaims;persistentvolumeclaims/status;pods;pods/attach;pods/binding;pods/eviction;pods/exec;pods/log;pods/portforward;pods/proxy;pods/status;replicasets;replicationcontrollers;replicationcontrollers/scale;replicationcontrollers/status;resourcequotas;resourcequotas/status;secrets;serviceaccounts;serviceaccounts/token;services;services/proxy;services/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions;customresourcedefinitions/status,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=controllerrevisions;daemonsets;daemonsets/status;deployments;deployments/scale;deployments/status;replicasets;replicasets/scale;replicasets/status;statefulsets;statefulsets/scale;statefulsets/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers;horizontalpodautoscalers/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=cronjobs;cronjobs/status;jobs;jobs/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses;ingresses;ingresses/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=spaces.samba.tv,resources=teams,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=spaces.samba.tv,resources=teams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=spaces.samba.tv,resources=teams/finalizers,verbs=update

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

	// Ensure namespace for Team resources exists.
	teamNamespace, err := kubeClient.CoreV1().Namespaces().Get(ctx, team.Spec.Namespace, apimetav1.GetOptions{})
	if err != nil {
		statusErr, ok := err.(*apierrors.StatusError)
		if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
			log.Error(err, "failure fetching team namespace")
			return ctrlruntime.Result{}, err
		}
		// Create team namespace.
		teamNamespace = &corev1.Namespace{
			ObjectMeta: apimetav1.ObjectMeta{
				Name:   team.Spec.Namespace,
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

	// Ensure unmanaged Team Role exists.
	teamRoleName := getTeamResourceName(teamName)
	teamRole, err := kubeClient.RbacV1().Roles(teamNamespace.Name).Get(ctx, teamRoleName, apimetav1.GetOptions{})
	if err != nil {
		statusErr, ok := err.(*apierrors.StatusError)
		if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
			log.Error(err, "failure fetching team role", "name", teamRoleName, "namespace", teamNamespace.Name)
			return ctrlruntime.Result{}, err
		}
		// Create Team implicit Role.
		rules := defaultRolePolicyRules
		if len(team.Spec.RolePolicyRules) > 0 {
			rules = team.Spec.RolePolicyRules
		}
		teamRole = &rbacv1.Role{
			ObjectMeta: apimetav1.ObjectMeta{
				Name:      teamRoleName,
				Namespace: teamNamespace.Name,
				Labels:    controllerLabels,
			},
			Rules: rules,
		}
		teamRole, err = kubeClient.RbacV1().Roles(teamNamespace.Name).Create(ctx, teamRole, apimetav1.CreateOptions{})
		if err != nil {
			log.Error(err, "failure creating team role", "name", teamRoleName, "namespace", teamNamespace.Name)
			return ctrlruntime.Result{}, err
		}
		log.Info("created team role")
	} else {
		log.Info("fetched team role", "name", teamRole.Name, "namespace", teamRole.Namespace)
	}

	// Ensure unmanaged Team ClusterRole exists.
	teamClusterRoleName := getTeamResourceName(teamName)
	teamClusterRole, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, teamClusterRoleName, apimetav1.GetOptions{})
	if err != nil {
		statusErr, ok := err.(*apierrors.StatusError)
		if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
			log.Error(err, "failure fetching team clusterrole", "clusterrole", teamClusterRoleName)
			return ctrlruntime.Result{}, err
		}
		// Create Team implicit ClusterRole.
		rules := defaultClusterRolePolicyRules
		if len(team.Spec.ClusterRolePolicyRules) > 0 {
			rules = team.Spec.ClusterRolePolicyRules
		}
		teamClusterRole = &rbacv1.ClusterRole{
			ObjectMeta: apimetav1.ObjectMeta{
				Name:      teamClusterRoleName,
				Namespace: teamNamespace.Name,
				Labels:    controllerLabels,
			},
			Rules: rules,
		}
		teamClusterRole, err = kubeClient.RbacV1().ClusterRoles().Create(ctx, teamClusterRole, apimetav1.CreateOptions{})
		if err != nil {
			log.Error(err, "failure creating team clusterrole", "name", teamClusterRoleName)
			return ctrlruntime.Result{}, err
		}
		log.Info("created team clusterrole", "name", teamClusterRole.Name)
	} else {
		log.Info("fetched team clusterrole", "name", teamClusterRole.Name)
	}

	// Ensure unmanaged Team RoleBinding exists.
	teamRoleBindingName := getTeamResourceName(teamName)
	teamRoleBinding, err := kubeClient.RbacV1().RoleBindings(teamNamespace.Name).Get(ctx, teamRoleBindingName, apimetav1.GetOptions{})
	if err != nil {
		statusErr, ok := err.(*apierrors.StatusError)
		if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
			log.Error(err, "failure fetching team rolebinding", "name", teamRoleBindingName, "namespace", teamNamespace.Name)
			return ctrlruntime.Result{}, err
		}
		// Create Team RoleBinding.
		teamRoleBinding = &rbacv1.RoleBinding{
			ObjectMeta: apimetav1.ObjectMeta{
				Name:      teamRoleBindingName,
				Namespace: teamNamespace.Name,
				Labels:    controllerLabels,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: RbacAPIGroup,
				Kind:     "Role",
				Name:     teamRole.Name,
			},
		}
		teamRoleBinding, err = kubeClient.RbacV1().RoleBindings(teamNamespace.Name).Create(ctx, teamRoleBinding, apimetav1.CreateOptions{})
		if err != nil {
			log.Error(err, "failure creating team rolebinding", "name", teamRoleBindingName, "namespace", teamNamespace.Name)
			return ctrlruntime.Result{}, err
		}
		log.Info("created team rolebinding", "name", teamRoleBinding.Name, "namespace", teamRoleBinding.Namespace)
	} else {
		log.Info("fetched team rolebinding", "name", teamRoleBinding.Name, "namespace", teamRoleBinding.Namespace)
	}

	// Ensure unmanaged Team ClusterRoleBinding exists.
	teamClusterRoleBindingName := getTeamResourceName(teamName)
	teamClusterRoleBinding, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, teamClusterRoleBindingName, apimetav1.GetOptions{})
	if err != nil {
		statusErr, ok := err.(*apierrors.StatusError)
		if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
			log.Error(err, "failure fetching team clusterrolebinding", "name", teamClusterRoleBindingName)
			return ctrlruntime.Result{}, err
		}
		// Create Team ClusterRoleBinding.
		teamClusterRoleBinding = &rbacv1.ClusterRoleBinding{
			ObjectMeta: apimetav1.ObjectMeta{
				Name:   teamClusterRoleBindingName,
				Labels: controllerLabels,
			},
			Subjects: []rbacv1.Subject{
				{
					APIGroup:  RbacAPIGroup,
					Kind:      "User",
					Name:      team.Spec.Username,
					Namespace: teamNamespace.Name,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: RbacAPIGroup,
				Kind:     "ClusterRole",
				Name:     teamClusterRole.Name,
			},
		}
		teamClusterRoleBinding, err = kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, teamClusterRoleBinding, apimetav1.CreateOptions{})
		if err != nil {
			log.Error(err, "failure creating team clusterrolebinding", "name", teamClusterRoleBindingName)
			return ctrlruntime.Result{}, err
		}
		log.Info("created team clusterrolebinding", "name", teamClusterRoleBinding.Name)
	} else {
		log.Info("fetched team clusterrolebinding", "name", teamClusterRoleBinding.Name)
	}

	// Ensure extra ClusterRoleBindings for Team, if any, exists.
	for _, extraTeamClusterRole := range team.Spec.ClusterRoleRefs {
		teamClusterRoleBindingName := getTeamResourceName(extraTeamClusterRole)
		teamClusterRoleBinding, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, teamClusterRoleBindingName, apimetav1.GetOptions{})
		if err != nil {
			statusErr, ok := err.(*apierrors.StatusError)
			if !ok || (ok && statusErr.ErrStatus.Reason != "NotFound") {
				log.Error(err, "failure fetching team clusterrolebinding", "name", teamClusterRoleBindingName)
				return ctrlruntime.Result{}, err
			}
			// Create Team ClusterRoleBinding.
			teamClusterRoleBinding = &rbacv1.ClusterRoleBinding{
				ObjectMeta: apimetav1.ObjectMeta{
					Name:   teamClusterRoleBindingName,
					Labels: controllerLabels,
				},
				Subjects: []rbacv1.Subject{
					{
						APIGroup:  RbacAPIGroup,
						Kind:      "User",
						Name:      team.Spec.Username,
						Namespace: teamNamespace.Name,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: RbacAPIGroup,
					Kind:     "ClusterRole",
					Name:     teamClusterRole.Name,
				},
			}
			teamClusterRoleBinding, err = kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, teamClusterRoleBinding, apimetav1.CreateOptions{})
			if err != nil {
				log.Error(err, "failure creating team clusterrolebinding", "name", teamClusterRoleBindingName)
				return ctrlruntime.Result{}, err
			}
			log.Info("created team clusterrolebinding", "name", teamClusterRoleBinding.Name)
		} else {
			log.Info("fetched team clusterrolebinding", "name", teamClusterRoleBinding.Name)
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

// getTeamResourceName returns a decorated name for a team resource.
func getTeamResourceName(resource string) string {
	return fmt.Sprintf("%s-%s", resource, "team")
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

// defaultClusterRolePolicyRules contains the default PolicyRules for the Team
// ClusterRole initially created by this controller but independent of the
// lifetime of the Team.
var defaultClusterRolePolicyRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{""}, // the core/v1 group
		Resources: []string{
			"namespaces",
			"namespaces/finalize",
			"namespaces/status",
			"nodes",
			"nodes/proxy",
			"nodes/status",
			"persistentvolumes",
			"persistentvolumes/status",
		},
		Verbs: []string{
			"get",
			"list",
			"watch",
		},
	},
	{
		APIGroups: []string{"apiextensions.k8s.io"},
		Resources: []string{
			"customresourcedefinitions",
			"customresourcedefinitions/status",
		},
		Verbs: []string{
			"get",
			"list",
			"watch",
		},
	},
	{
		APIGroups: []string{RbacAPIGroup},
		Resources: []string{
			"clusterrolebindings",
			"clusterroles",
		},
		Verbs: []string{
			"get",
			"list",
			"watch",
		},
	},
}

// defaultRolePolicyRules contains the default PolicyRules for the Team Role
// initially created by this controller but independent of the lifetime of the Team.
var defaultRolePolicyRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{""}, // the core/v1 group
		Resources: []string{
			"bindings",
			"configmaps",
			"endpoints",
			"events",
			"persistentvolumeclaims",
			"persistentvolumeclaims/status",
			"pods",
			"pods/attach",
			"pods/binding",
			"pods/eviction",
			"pods/exec",
			"pods/log",
			"pods/portforward",
			"pods/proxy",
			"pods/status",
			"replicationcontrollers",
			"replicationcontrollers/scale",
			"replicationcontrollers/status",
			"resourcequotas",
			"resourcequotas/status",
			"secrets",
			"serviceaccounts",
			"serviceaccounts/token",
			"services",
			"services/proxy",
			"services/status",
		},
		Verbs: []string{
			"get",
			"list",
			"watch",
			"create",
			"update",
			"patch",
			"delete",
		},
	},
	{
		APIGroups: []string{"apps"},
		Resources: []string{
			"controllerrevisions",
			"daemonsets",
			"daemonsets/status",
			"deployments",
			"deployments/scale",
			"deployments/status",
			"replicasets",
			"replicasets/scale",
			"replicasets/status",
			"statefulsets",
			"statefulsets/scale",
			"statefulsets/status",
		},
		Verbs: []string{
			"get",
			"list",
			"watch",
			"create",
			"update",
			"patch",
			"delete",
		},
	},
	{
		APIGroups: []string{"autoscaling"},
		Resources: []string{
			"horizontalpodautoscalers",
			"horizontalpodautoscalers/status",
		},
		Verbs: []string{
			"get",
			"list",
			"watch",
			"create",
			"update",
			"patch",
			"delete",
		},
	},
	{
		APIGroups: []string{"batch"},
		Resources: []string{
			"cronjobs",
			"cronjobs/status",
			"jobs",
			"jobs/status",
		},
		Verbs: []string{
			"get",
			"list",
			"watch",
			"create",
			"update",
			"patch",
			"delete",
		},
	},
	{
		APIGroups: []string{"networking.k8s.io"},
		Resources: []string{
			"ingressclasses",
			"ingresses",
			"ingresses/status",
		},
		Verbs: []string{
			"get",
			"list",
			"watch",
			"create",
			"update",
			"patch",
			"delete",
		},
	},
	{
		APIGroups: []string{RbacAPIGroup},
		Resources: []string{
			"rolebindings",
			"roles",
		},
		Verbs: []string{
			"get",
			"list",
			"watch",
		},
	},
}

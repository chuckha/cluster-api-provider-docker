/*
Copyright 2019 The Kubernetes Authors.

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

package objects

import (
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterapiv1alpha2 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha2"
)

const clusterRoleName = "docker-provider-manager-role"

// GetClusterRole returns the cluster role clusterapiv1alpha2 needs to function properly
func GetClusterRole() rbac.ClusterRole {
	return rbac.ClusterRole{
		ObjectMeta: meta.ObjectMeta{
			Name: clusterRoleName,
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups: []string{
					clusterapiv1alpha2.SchemeGroupVersion.Group,
				},
				Resources: []string{
					"clusters",
					"clusters/status",
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
				APIGroups: []string{
					clusterapiv1alpha2.SchemeGroupVersion.Group,
				},
				Resources: []string{
					"machines",
					"machines/status",
					"machinedeployments",
					"machinedeployments/status",
					"machinesets",
					"machinesets/status",
					"machineclasses",
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
				APIGroups: []string{
					core.GroupName,
				},
				Resources: []string{
					"nodes",
					"events",
					"secrets",
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
		},
	}
}

// GetClusterRoleBinding returns the binding for the role created by GetClusterRole
func GetClusterRoleBinding() rbac.ClusterRoleBinding {
	return rbac.ClusterRoleBinding{
		ObjectMeta: meta.ObjectMeta{
			Name: "docker-provider-manager-rolebinding",
		},
		RoleRef: rbac.RoleRef{
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
			APIGroup: rbac.GroupName,
		},
		Subjects: []rbac.Subject{{
			Kind:      rbac.ServiceAccountKind,
			Name:      "default",
			Namespace: namespace,
		}},
	}
}

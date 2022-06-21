# sambatv/spaces-operator

This repository contains the Golang implementation of a Kubernetes Operator
managing [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
spaces for teams, built with [kubebuilder](https://kubebuilder.io/).

## Custom Resource Definitions

This operator provides the following CRD kinds in the `spaces.samba.tv` API group.

- [Team](config/samples/team.yaml)

## External Resources

- [Kubebuilder documentation](https://book.kubebuilder.io/)
- [kubernetes Custom Resources documentation](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
- [Kubernetes Controllers documentation](https://kubernetes.io/docs/concepts/architecture/controller/)
- [Kubernetes Operator documentation](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- [Kubernetes Role-Based Access Control documentation](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)

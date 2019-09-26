module sigs.k8s.io/cluster-api-provider-docker

go 1.12

require (
	github.com/chuckha/cluster-api-lib v0.0.0-00010101000000-000000000000
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/pkg/errors v0.8.1
	k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/klog v0.4.0
	sigs.k8s.io/cluster-api v0.2.2
	sigs.k8s.io/controller-runtime v0.2.2
	sigs.k8s.io/kind v0.5.1
)

replace (
	github.com/chuckha/cluster-api-lib => ../cluster-api-lib
	sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v0.0.0-20190829144357-1063658f9b58
)

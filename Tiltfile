project = str(local('gcloud config get-value project')).strip()

# let's go~~~~
enable_feature("team_alerts")

read_file(str(local('which capdctl')).rstrip('\n'))
k8s_yaml(local('capdctl platform -capi-image gcr.io/kubernetes1-226021/cluster-api-controller-amd64:dev -bp-image gcr.io/kubernetes1-226021/cluster-api-bootstrap-provider-kubeadm:dev -bp-ref master'))


docker_build('gcr.io/' + project + '/cluster-api-controller-amd64', '../../go/src/sigs.k8s.io/cluster-api')
docker_build('gcr.io/' + project + '/cluster-api-bootstrap-provider-kubeadm', '../cluster-api-bootstrap-provider-kubeadm')
docker_build('gcr.io/' + project +'/manager', '.', live_update=[
    # todo we probably want more than just controllers here
    sync('./controllers', '/home/ubuntu/cluster-api-provider-docker/controllers'),
    # TODO: add a run() and redeploy when manifests change? make manifests => reapply rbac?
    # for containerd
    run('/restart.sh'),
])

# image-clone-controller

To deploy this on a cluster, modify the credentials in the deploy/secret.yaml file and run `kubectl apply -f deploy/`
It backs up images to specified registry, in the case it defaults to `kofoworola` on dockerhub.

It skips the deployments and daemonsets in the kube-system namespace but more namespaces can be skipped via a cli arg `--ignore-namespaces`

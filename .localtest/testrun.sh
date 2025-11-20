cd ../cluster-autoscaler || exit

op run --env-file ../.localtest/.env -- /bin/sh -c 'go run main.go \
  --kubeconfig "$KUBECONFIG_FILE" \
  --cloud-provider=exoscale \
  --namespace=kube-system \
  --ignore-daemonsets-utilization=true \
  --logtostderr=true \
  --scale-down-unneeded-time=3m \
  --scale-down-utilization-threshold=0.7 \
  --skip-nodes-with-local-storage=false \
  --skip-nodes-with-system-pods=false \
  --stderrthreshold=info \
  --v=4 \
  --write-status-configmap=true'


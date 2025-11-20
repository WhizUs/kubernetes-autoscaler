op run --env-file .env -- /bin/sh -c 'exo compute sks kubeconfig \
  $CLUSTER_NAME default -g "system:masters" \
  --ttl 315360000 \
  -z "$EXOSCALE_ZONE" >"$KUBECONFIG_FILE"'

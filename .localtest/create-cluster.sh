#!/bin/sh

op run --no-masking --env-file .env -- /bin/sh -c 'exo compute sks create $CLUSTER_NAME \
--zone at-vie-1 \
--kubernetes-version "1.34.1" \
--nodepool-name default \
--nodepool-size 1 \
--nodepool-instance-type "standard.small" \
--nodepool-disk-size 20 \
--service-level "starter"'


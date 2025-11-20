#!/bin/sh

op run --env-file .env -- /bin/sh -c 'exo compute sks delete $CLUSTER_NAME --nodepools --zone at-vie-1 --force'


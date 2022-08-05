#!/bin/bash

if [[ "$1" == "clean" ]]; then
    kubectl delete -f db_shard.yaml
    kubectl delete -f db_replica.yaml
    kubectl delete -f tls/issuer.yaml
    sleep 30
    kubectl delete ns db
    sleep 5
fi 

kubectl create ns db
kubectl apply -f cm.yaml
kubectl apply -f tls/secret.yaml
kubectl apply -f tls/issuer.yaml
kubectl create secret generic -n db custom-config --from-file=./mongod.conf
# kubectl apply -f db_shard.yaml
kubectl apply -f db_replica.yaml

# secret/custom-config                        Opaque                                1      8m25s
# secret/default-token-b2mk4                  kubernetes.io/service-account-token   3      8m25s
# secret/mongo-ca                             kubernetes.io/tls                     2      8m25s

# secret/mongodb-auth                         Opaque                                2      8m24s
# secret/mongodb-client-cert                  kubernetes.io/tls                     3      8m22s
# secret/mongodb-configsvr-server-cert        kubernetes.io/tls                     3      8m24s
# secret/mongodb-metrics-exporter-cert        kubernetes.io/tls                     3      8m22s
# secret/mongodb-mongos-server-cert           kubernetes.io/tls                     3      8m24s
# secret/mongodb-shard0-arbiter-server-cert   kubernetes.io/tls                     3      8m24s
# secret/mongodb-shard0-server-cert           kubernetes.io/tls                     3      8m24s
# secret/mongodb-shard1-arbiter-server-cert   kubernetes.io/tls                     3      8m22s
# secret/mongodb-shard1-server-cert           kubernetes.io/tls                     3      8m24s
# secret/mongodb-token-967b4                  kubernetes.io/service-account-token   3      8m24s





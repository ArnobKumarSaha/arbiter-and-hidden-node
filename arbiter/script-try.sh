#!/bin/bash




function get_peers_for_replicaset {
    local HOSTS=$(echo "$1" | tr "/" "\n")
    # convert to an array
    local pods=($HOSTS)
    # first index contains the string "replicaset". removing it
    unset pods[0]
    # pods are comma separated. make it an array.
    local HOSTS=$(echo "${pods[@]}" | tr "," "\n")
    peers=($HOSTS)
}

# get_peers_for_replicaset "shard0/mongo-shard0-0.mongo-shard0-pods.db.svc:27017,mongo-shard0-1.mongo-shard0-pods.db.svc:27017,mongo-shard0-2.mongo-shard0-pods.db.svc:27017"

function get_pods {
    local reps=($1)
    for rep in "${reps[@]}"; do
        echo "rep = $rep"
        local HOSTS=$(echo "$rep" | tr "/" "\n")
        local pods=($HOSTS)
        echo "${pods[0]}"
        if [[ "${pods[0]}" != "$REPLICA_SET" ]]; then
            continue
        fi
        unset pods[0]
        # pods are comma separated. make it an array.
        local HOSTS=$(echo "${pods[@]}" | tr "," "\n")
        local ps=($HOSTS)
        echo "2 ->" "${ps[@]}"
        for pod in "${ps[@]}"; do
            echo "$pod"
        done
        peers=(${peers[@]} ${ps[@]})
    done
}

# REPLICA_SET=shard0
# # SHARD_REPSETS='shard0/mongo-shard0-0.mongo-shard0-pods.db.svc:27017,mongo-shard0-1.mongo-shard0-pods.db.svc:27017,mongo-shard0-2.mongo-shard0-pods.db.svc:27017 shard1/mongo-shard1-0.mongo-shard1-pods.db.svc:27017,mongo-shard1-1.mongo-shard1-pods.db.svc:27017,mongo-shard1-2.mongo-shard1-pods.db.svc:27017'
# SHARD_REPSETS='shard0/s00,s01,s02 shard1/s10,s11,s12'
# # get_pods "$SHARD_REPSETS"
# echo ${peers[@]}

# if [[ "$REPLICA_SET" == 'shard' ]]; then
#     echo hi
# else echo bla 
# fi


function get_peers_for_sharded_cluster {
   local reps=($1)
   for rep in "${reps[@]}"; do
       log "rep = $rep"
       local HOSTS=$(echo "$rep" | tr "/" "\n")
       local pods=($HOSTS)
       if [[ "${pods[0]}" != "$REPLICA_SET" ]]; then
           continue
       fi
       unset pods[0]
       # pods are comma separated. make it an array.
       local HOSTS=$(echo "${pods[@]}" | tr "," "\n")
       local ps=($HOSTS)
       for pod in "${ps[@]}"; do
           log "$pod"
       done
       peers=(${peers[@]} ${ps[@]})
   done
}


# CONFIGDB_REPSET=cnfRepSet/mongo-configsvr-0.mongo-configsvr-pods.db.svc:27017,
# mongo-configsvr-1.mongo-configsvr-pods.db.svc:27017,
# mongo-configsvr-2.mongo-configsvr-pods.db.svc:27017

# SHARD_REPSETS=shard0/mongo-shard0-0.mongo-shard0-pods.db.svc:27017,
# mongo-shard0-1.mongo-shard0-pods.db.svc:27017,
# mongo-shard0-2.mongo-shard0-pods.db.svc:27017 
# shard1/mongo-shard1-0.mongo-shard1-pods.db.svc:27017,
# mongo-shard1-1.mongo-shard1-pods.db.svc:27017,
# mongo-shard1-2.mongo-shard1-pods.db.svc:27017
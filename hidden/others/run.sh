
#!/bin/bash

my_hostname=sh-shard0-hidden-2
HIDDEN_REPLICA_COUNT=4
GOVERNING_SERVICE_NAME=gvgv
POD_NAMESPACE=ns

for (( c=1; c<=$HIDDEN_REPLICA_COUNT; c++ )); do
    cur_host=$(echo $my_hostname | sed -e "s/[-][0-9]*$/-${c}.${GOVERNING_SERVICE_NAME}.${POD_NAMESPACE}.svc/") 
    echo $c $cur_host
done

exit 0

conf=$(cat actual.conf)
#conf=$(cat modified.conf)


echo "$conf" > conf.json

for var in "NumberLong" "ObjectId";do 
    echo $var 
    sed -i  -e "/$var/s/[)]//" -e "/$var/s/${var}[(]//" conf.json
    # sed -i  -e '/NumberLong/s/NumberLong[(]//' conf.json
done



x=$(cat conf.json | jq '.members[]')
# x=$(echo "$conf" | jq -r '.settings')
# echo "$x"

service_name=mg-rs-hidden-0.mg-rs-pods.demo.svc.cluster.local

readarray -t my_array < <(jq -c '.members[]' conf.json)
for item in "${my_array[@]}"; do
  host=$(jq '.host' <<< "$item")
  hidden=$(jq '.hidden' <<< "$item")

  # remove the quotation marks from the start & end
  host=$(echo $host | sed -e 's/^"//' -e 's/"$//')
  echo "$host" "$hidden"
  # host=$(echo $host | sed -e 's/[:][0-9]*//')
  host=${host//[:][0-9]*/}
  host=${host//[:][0-9]*/}
  echo "$host"

  if [[ "$host" == "$service_name" && "$hidden" == true ]]; then
    echo "yaaa"
  fi 
done


rm conf.json

#!/bin/bash


conf=$(cat actual.conf)
#conf=$(cat modified.conf)


echo "$conf" > conf.json

for var in "NumberLong" "ObjectId";do 
    echo $var 
    sed -i  -e "/$var/s/[)]//" -e "/$var/s/$var[(]//" conf.json
    # sed -i  -e '/NumberLong/s/NumberLong[(]//' conf.json
done



x=$(cat conf.json | jq '.members[]')
# x=$(echo "$conf" | jq -r '.settings')
# echo "$x"

service_name=mg-rs-hidden-0.mg-rs-pods.demo.svc.cluster.local:27017

readarray -t my_array < <(jq -c '.members[]' conf.json)
for item in "${my_array[@]}"; do
  host=$(jq '.host' <<< "$item")
  hidden=$(jq '.hidden' <<< "$item")

  # remove the quotation marks from the start & end
  host=$(echo $host | sed -e 's/^"//' -e 's/"$//')
  echo "$host" "$hidden"

  if [[ "$host" == "$service_name" && "$hidden" == true ]]; then
    echo "yaaa"
  fi 
done



rm conf.json
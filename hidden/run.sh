
#!/bin/bash


conf=$(cat actual.conf)
#conf=$(cat modified.conf)


echo "$conf" > conf.json

# x=$(cat conf.json | jq '.settings')
x=$(echo "$conf" | jq -r '.settings')
echo "$x"



# readarray -t my_array < <(jq -c '.[]' "$conf")
# for item in "${my_array[@]}"; do
#   original_name=$(jq '.version' <<< "$item")
#   echo "$original_name"
# done

# mem=$(echo "${conf}" | jq -r '.')
# echo "$mem"

rm conf.json
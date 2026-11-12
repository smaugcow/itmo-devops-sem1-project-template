#!/usr/bin/env bash
set -euo pipefail
project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
: "${YC_PROFILE:?set a personal yc profile}"
: "${YC_CLOUD_ID:?}" "${YC_FOLDER_ID:?}" "${YC_SUBNET_ID:?}" "${YC_ZONE:?}" "${YC_SECURITY_GROUP_ID:?}"
: "${VM_NAME:?use n- followed by a persisted UUID}" "${SSH_PUBLIC_KEY_FILE:?}" "${SSH_PRIVATE_KEY_FILE:?}"
: "${CONFIRM_CREATE:?set yes only after approving resources and cost}"
[[ "$CONFIRM_CREATE" == yes ]]
[[ "$VM_NAME" =~ ^n-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]
case "$YC_PROFILE" in bm-*|haas-*|default) echo 'use a personal profile' >&2; exit 1;; esac
ssh_user=${SSH_USER:-ubuntu}
[[ "$ssh_user" =~ ^[a-z][a-z0-9_-]*$ ]]
state_dir=${STATE_DIR:-$project_root/.runtime/$VM_NAME}
mkdir -p "$state_dir"
chmod 700 "$state_dir"
umask 077
cloud=(yc --profile "$YC_PROFILE" --cloud-id "$YC_CLOUD_ID" --folder-id "$YC_FOLDER_ID" --format json)
"${cloud[@]}" vpc subnet get --id "$YC_SUBNET_ID" > "$state_dir/subnet.json"
jq -e --arg zone "$YC_ZONE" --arg folder "$YC_FOLDER_ID" '.zone_id==$zone and .folder_id==$folder' "$state_dir/subnet.json" >/dev/null
"${cloud[@]}" vpc security-group get --id "$YC_SECURITY_GROUP_ID" > "$state_dir/group.json"
jq -e --arg network "$(jq -r .network_id "$state_dir/subnet.json")" '.network_id==$network' "$state_dir/group.json" >/dev/null
"${cloud[@]}" compute instance list > "$state_dir/instances.json"
count=$(jq --arg name "$VM_NAME" '[.[]|select(.name==$name)]|length' "$state_dir/instances.json")
if [[ "$count" == 0 ]]; then
  if [[ -e "$state_dir/instance-id" ]]; then echo 'recorded VM missing; refusing replacement' >&2; exit 1; fi
  jq -n --arg name "$VM_NAME" --arg user "$ssh_user" --rawfile key "$SSH_PUBLIC_KEY_FILE" '{hostname:$name,ssh_pwauth:false,disable_root:true,write_files:[{path:"/etc/ssh/sshd_config.d/00-final.conf",permissions:"0644",content:"PasswordAuthentication no\nPermitRootLogin no\n"}],users:[{name:$user,groups:["sudo"],shell:"/bin/bash",sudo:"ALL=(ALL) NOPASSWD:ALL",ssh_authorized_keys:[$key|rtrimstr("\n")]}],output:{all:"| tee -a /var/log/cloud-init-output.log /dev/console"},runcmd:[["systemctl","reload","ssh"],["sh","-c","printf '\''FINAL_HOST_KEY '\''; cat /etc/ssh/ssh_host_ed25519_key.pub"]]}' > "$state_dir/cloud.json"
  { printf '#cloud-config\n'; cat "$state_dir/cloud.json"; } > "$state_dir/cloud-init.yaml"
  "${cloud[@]}" compute instance create --name "$VM_NAME" --hostname "$VM_NAME" --zone "$YC_ZONE" --cores 2 --memory 2GB --preemptible --labels "operation=$VM_NAME" --create-boot-disk "name=$VM_NAME,size=20GB,image-folder-id=standard-images,image-family=ubuntu-2404-lts" --network-interface "subnet-id=$YC_SUBNET_ID,nat-ip-version=ipv4,security-group-ids=$YC_SECURITY_GROUP_ID" --metadata-from-file "user-data=$state_dir/cloud-init.yaml" > "$state_dir/instance.json"
else
  [[ "$count" == 1 ]]
  jq --arg name "$VM_NAME" '.[]|select(.name==$name)' "$state_dir/instances.json" > "$state_dir/instance.json"
fi
jq -e --arg name "$VM_NAME" --arg folder "$YC_FOLDER_ID" '.labels.operation==$name and .folder_id==$folder and .name==$name' "$state_dir/instance.json" >/dev/null
instance_id=$(jq -r .id "$state_dir/instance.json")
if [[ -e "$state_dir/instance-id" ]]; then [[ "$(cat "$state_dir/instance-id")" == "$instance_id" ]]; fi
printf '%s\n' "$instance_id" > "$state_dir/instance-id"
ip=$(jq -r '.network_interfaces[0].primary_v4_address.one_to_one_nat.address' "$state_dir/instance.json")
[[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]
for attempt in $(seq 1 60); do
  host_key=$("${cloud[@]}" compute instance get-serial-port-output --id "$instance_id" --port 1 | jq -r '.contents' | sed -n 's/^.*FINAL_HOST_KEY \(ssh-ed25519 [A-Za-z0-9+/=]*\).*$/\1/p' | tail -n 1)
  if [[ "$host_key" =~ ^ssh-ed25519\ [A-Za-z0-9+/=]+$ ]]; then break; fi
  sleep 5
done
[[ "$host_key" =~ ^ssh-ed25519\ [A-Za-z0-9+/=]+$ ]]
printf '%s %s\n' "$ip" "$host_key" > "$state_dir/known_hosts"
ssh_options=(-i "$SSH_PRIVATE_KEY_FILE" -o IdentitiesOnly=yes -o BatchMode=yes -o ForwardAgent=no -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$state_dir/known_hosts" -o ConnectTimeout=10)
ready=false
for attempt in $(seq 1 30); do
  if ssh "${ssh_options[@]}" "$ssh_user@$ip" true; then ready=true; break; fi
  sleep 5
done
[[ "$ready" == true ]]
ssh "${ssh_options[@]}" "$ssh_user@$ip" 'sudo cloud-init status --wait >/dev/null && mkdir -p ~/prices' </dev/null
ssh "${ssh_options[@]}" "$ssh_user@$ip" 'bash -s' < "$project_root/scripts/install-docker.sh" >&2
image=${APP_IMAGE:-final-prices:local}
[[ "$image" =~ ^[a-zA-Z0-9_./:-]+$ ]]
docker save "$image" | ssh "${ssh_options[@]}" "$ssh_user@$ip" 'sudo docker load' >&2
scp "${ssh_options[@]}" "$project_root/compose.yml" "$ssh_user@$ip:prices/compose.yml" >&2
ssh "${ssh_options[@]}" "$ssh_user@$ip" "APP_IMAGE=$image APP_BIND=0.0.0.0 sudo --preserve-env=APP_IMAGE,APP_BIND docker compose --project-name prices -f /home/$ssh_user/prices/compose.yml up -d" </dev/null >&2
ready=false
for attempt in $(seq 1 30); do
  if curl --fail --silent --max-time 5 "http://$ip:8080/health" >/dev/null; then ready=true; break; fi
  sleep 3
done
[[ "$ready" == true ]]
printf '%s\n' "$ip"

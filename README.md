## install ansible

### Ubuntu
```shell
sudo apt update && sudo apt upgrade -y
sudo apt install software-properties-common -y
sudo add-apt-repository --yes --update ppa:ansible/ansible
sudo apt install ansible -y
ansible --version
```

### Rocky 9.6
```shell
sudo dnf update -y
sudo dnf install epel-release -y
sudo dnf config-manager --set-enabled crb
sudo dnf install ansible -y
ansible --version
```

## Requirement
remember to install this:
```shell
ansible-galaxy collection install community.postgresql community.mysql community.mongodb
```

## How to use
create a file under the inventories directory that describes the name of the host.
example: inventories/db-prod.ini

```yaml
192.168.1.10 ansible_user=rocky ansible_password=yourPass db_name=appdb db_user=appuser db_password=AppP@ssw0rd!
```

running on a specific host
```shell
ansible -i inventories/db-prod.ini -m ping
ansible-playbook -i inventories/db-prod.ini playbooks/postgresql.yml
```

1. build go-ansible-executor

```shell
cd ansible-automation-script/go-ansible-executor

# cross-compile for Rocky 9.6 (Linux amd64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../ansible-executor
```

now there will be
```
ansible-automation-script/
├── ansible-executor   ✅ compiled Go binary
├── go-ansible-executor/
│   ├── go.mod
│   └── main.go
├── inventories/
├── playbooks/
```

2. Copy to Rocky Linux

On the server:

```shell
# Create app directories
# service-only user (no home, nologin)
sudo useradd --system --shell /sbin/nologin ansiblex

# app dirs
sudo mkdir -p /opt/ansible-executor/{bin,playbooks,inventories,.ansible/tmp}

# permissions (service user owns the tree)
sudo chown -R ansiblex:ansiblex /opt/ansible-executor
sudo chmod 0755 /opt/ansible-executor
sudo chmod 0755 /opt/ansible-executor/{bin,playbooks}
sudo chmod 0700 /opt/ansible-executor/inventories
sudo chmod 0700 /opt/ansible-executor/.ansible
sudo chmod 0700 /opt/ansible-executor/.ansible/tmp

sudo mkdir -p /var/tmp/.ansible-root/tmp
sudo chown root:root /var/tmp/.ansible-root/tmp
sudo chmod 700 /var/tmp/.ansible-root/tmp
```

On your Mac:
```shell
scp ansible-executor root@10.2.0.159:/opt/ansible-executor/bin/
scp ansible.cfg root@10.2.0.159:/opt/ansible-executor/
scp -r playbooks root@10.2.0.159:/opt/ansible-executor/
```

On server:
```shell
sudo chmod 0755 /opt/ansible-executor/bin/ansible-executor
sudo chown -R ansiblex:ansiblex /opt/ansible-executor
```

3. Create a systemd unit

File: /etc/systemd/system/ansible-executor.service
```
[Unit]
Description=Go Ansible Executor (NATS -> Ansible)
After=network-online.target
Wants=network-online.target

[Service]
User=ansiblex
Group=ansiblex
WorkingDirectory=/opt/ansible-executor

# Environment variables
Environment="HOME=/opt/ansible-executor"
Environment="ANSIBLE_CONFIG=/opt/ansible-executor/ansible.cfg"
Environment="ANSIBLE_LOCAL_TEMP=/opt/ansible-executor/.ansible/tmp"
Environment="ANSIBLE_REMOTE_TEMP=/tmp"
Environment="ANSIBLE_HOST_KEY_CHECKING=False"
Environment="NATS_URL=nats://127.0.0.1:4222"
Environment="PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin"

ExecStart=/opt/ansible-executor/bin/ansible-executor

Restart=always
RestartSec=5s

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/opt/ansible-executor/inventories

StandardOutput=journal
StandardError=journal
SyslogIdentifier=ansible-executor

[Install]
WantedBy=multi-user.target
```

4. Enable & start
```shell
sudo systemctl daemon-reload
sudo systemctl enable --now ansible-executor.service
systemctl status ansible-executor.service
journalctl -u ansible-executor -f
```

5. Test

Publish a message to NATS:
```shell
nats pub db.install '{
  "id": 16,
  "name": "DB Prod PostgreSQL",
  "ip_address": "10.2.0.118",
  "vm_user": "root",
  "vm_password": "P@ssw0rd123!!",
  "db_type": "postgresql",
  "db_user": "appuser",
  "db_password": "appPassword",
  "db_name": "app_db" 
}'
```

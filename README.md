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
mkdir -p /opt/ansible-executor/bin
mkdir -p /opt/ansible-executor/inventories
mkdir -p /opt/ansible-executor/playbooks

# (optional) create a dedicated user
useradd --system --no-create-home --shell /sbin/nologin ansiblex

# Give ownership to ansiblex
chown -R ansiblex:ansiblex /opt/ansible-executor
```

On your Mac:
```shell
scp ansible-executor root@10.2.0.82:/opt/ansible-executor/bin/
scp ansible.cfg root@10.2.0.82:/opt/ansible-executor/
scp -r playbooks root@10.2.0.82:/opt/ansible-executor/
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
    "id": 1,
    "name": "DB PostgreSQL HiTeman Prod",
    "ip_address": "10.2.0.61",
    "vm_user": "root",
    "vm_password": "P@ssw0rd123!!",
    "db_type": "postgresql",
    "db_user": "appUser",
    "db_password": "appPassword",
    "db_name": "app_db"
}'
```

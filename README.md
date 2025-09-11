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
192.168.1.10 ansible_user=rocky ansible_password=yourPass ansible_become_password=yourSudoPass
```

running on a specific host
```shell
ansible -i inventories/db-prod.ini -m ping
ansible-playbook -i inventories/db-prod.ini playbooks/postgres.yml
```

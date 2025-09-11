remember to install this:
```shell
ansible-galaxy collection install community.postgresql community.mysql community.mongodb
```

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

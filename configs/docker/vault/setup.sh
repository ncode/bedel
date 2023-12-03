#!/bin/sh

set -x

setup_vault(){
    # Wait for Vault server to be up
    echo "Waiting for Vault to start..."
    while ! nc -z localhost 8200; do
      sleep 1
    done

    echo "Vault started"

    vault login $VAULT_DEV_ROOT_TOKEN_ID

    # Enable database secret engine
    vault secrets enable database

    sleep 1

    instances="redis0001 redis0002"
    for instance in $instances ; do
        vault write "database/config/${instance}" \
            plugin_name="redis-database-plugin" \
            host=$instance \
            port=6379 \
            tls=false \
            username="default" \
            password="bedel-integration-test" \
            allowed_roles="*-${instance}"

        vault write "database/roles/admin-${instance}" \
            db_name=$instance \
            creation_statements='["+@admin"]' \
            default_ttl="30m" \
            max_ttl="1h"
    done

    for i in $(seq 1 10) ; do
        vault read database/creds/admin-redis0001
        vault read database/creds/admin-redis0002
    done
    echo "Vault configuration complete"
}

setup_vault &

# start dev vault
vault server -dev -dev-root-token-id=$VAULT_DEV_ROOT_TOKEN_ID

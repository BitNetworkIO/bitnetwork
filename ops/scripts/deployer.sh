#!/bin/bash
set -euo

RETRIES=${RETRIES:-20}
JSON='{"jsonrpc":"2.0","id":0,"method":"net_version","params":[]}'

if [ -z "$CONTRACTS_RPC_URL" ]; then
    echo "Must specify \$CONTRACTS_RPC_URL."
    exit 1
fi

# wait for the base layer to be up
curl \
    --fail \
    --show-error \
    --silent \
    -H "Content-Type: application/json" \
    --retry-connrefused \
    --retry $RETRIES \
    --retry-delay 1 \
    -d $JSON \
    $CONTRACTS_RPC_URL > /dev/null

echo "Connected to L1."
echo "Building deployment command."

echo $CONTRACTS_TARGET_NETWORK
echo $SKIP_CONTRACT_DEPLOY
echo "test point1"

if [ $CONTRACTS_TARGET_NETWORK == "local" ] ;then
  DEPLOY_CMD="npx hardhat deploy --network $CONTRACTS_TARGET_NETWORK"

#  # todo: remove
#  # whiteListInit
#  WHITELIST_INIT="npx hardhat whiteListInit --rollup $ROLLUP_CONTRACT_ADDRESS"
#  # fraud proof verifier deploy
#  DEPLOY_VERIFIER="npx hardhat deployVerifier --verifier $VERIFIER_ENTRY_ADDRESS"

  echo "Deploying contracts. Deployment command:"
  echo "$DEPLOY_CMD"
  eval "$DEPLOY_CMD"

#  echo "Deploying fraud proof verifiers. Deployment command:"
#  echo "$DEPLOY_VERIFIER"
#  eval "$DEPLOY_VERIFIER"
#  echo "Init fraud proof whitelists. Init command:"
#  echo "$WHITELIST_INIT"
#  eval "$WHITELIST_INIT"


elif [ $SKIP_CONTRACT_DEPLOY == "NO" ] ; then
  DEPLOY_CMD="npx hardhat deploy --network $CONTRACTS_TARGET_NETWORK"
  echo $PWD
  rm -rf deployments/goerli-qa
  rm -rf deployments/goerli-testnet
  rm -rf deployments/goerlibn

  echo "Deploying contracts. Deployment command:"
  echo "$DEPLOY_CMD"
  eval "$DEPLOY_CMD"
else [ $SKIP_CONTRACT_DEPLOY == "YES" ]
   sleep 30
fi

echo "Building addresses.json."
export ADDRESS_MANAGER_ADDRESS=$(cat "./deployments/$CONTRACTS_TARGET_NETWORK/Lib_AddressManager.json" | jq -r .address)


if [ $SKIP_CONTRACT_DEPLOY == "NO" ] ;then
  echo "Re-generate addresses.txt"
  # First, create two files. One of them contains a list of addresses, the other contains a list of contract names.
  find "./deployments/$CONTRACTS_TARGET_NETWORK" -maxdepth 1 -name '*.json' | xargs cat | jq -r '.address' > addresses.txt
  find "./deployments/$CONTRACTS_TARGET_NETWORK" -maxdepth 1 -name '*.json' | sed -e "s/.\/deployments\/$CONTRACTS_TARGET_NETWORK\///g" | sed -e 's/.json//g' > filenames.txt
elif [ $CONTRACTS_TARGET_NETWORK == "goerli-qa" ] ; then
  cp -r addresses-qa.txt addresses.txt
  cp -r filenames-qa.txt filenames.txt
else [ $CONTRACTS_TARGET_NETWORK == "goerli-testnet" ]
  cp -r addresses-testnet.txt addresses.txt
  cp -r filenames-testnet.txt filenames.txt
fi

# only gen addresses.json in local. Use exist configmap in k8s environment
if [ $SKIP_CONTRACT_DEPLOY = "NO" ];then
  # Start building addresses.json.
  echo "{" > addresses.json
  # Zip the two files describe above together, then, switch their order and format as JSON.
  paste addresses.txt filenames.txt | awk '{printf "  \"%s\": \"%s\",\n", $2, $1}' >> addresses.json
  # Add the address manager alias.
  echo "\"AddressManager\": \"$ADDRESS_MANAGER_ADDRESS\"" >> addresses.json
  # End addresses.json
  echo "}" >> addresses.json
fi

echo "Built addresses.json. Content:"
jq . addresses.json

echo "Building dump file."
npx hardhat take-dump --network $CONTRACTS_TARGET_NETWORK
cp addresses.json ./genesis
cp ./genesis/$CONTRACTS_TARGET_NETWORK.json ./genesis/state-dump.latest.json

# init balance
if [ $CONTRACTS_TARGET_NETWORK == "local" ] ;then
  jq -n 'reduce inputs as $item ({}; . *= $item)' ./genesis/state-dump.latest.json ./balance.json > genesis2.json
  mv ./genesis/state-dump.latest.json ./genesis/state-dump.latest.json.bak
  cp ./genesis2.json ./genesis/state-dump.latest.json
fi


# service the addresses and dumps
echo "Starting server."
exec python3 -m http.server \
    --bind "0.0.0.0" 8081 \
    --directory ./genesis

#!/bin/sh
set -e

echo "Installing curl"
apt update
apt install curl -y

echo "Installing helm"
curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash
helm init -c

echo "Packaging charts from source code"
mkdir -p temp
for d in charts/*
do
 # shellcheck disable=SC2039
 if [[ -d $d ]]
 then
    # Will generate a helm package per chart in a folder
    echo "$d"
    helm package "$d"
    # shellcheck disable=SC2035
    mv *.tgz temp/
  fi
done

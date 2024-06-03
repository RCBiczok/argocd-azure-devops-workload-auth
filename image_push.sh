#!/bin/bash

set -e

echo "### Building Image ..."

docker build -t argocd-azure-devops-workload-auth .
docker tag argocd-azure-devops-workload-auth "lupustech/argocd-azure-devops-workload-auth:1.0.0"

echo "### Docker Push ..."

docker push lupustech/argocd-azure-devops-workload-auth:1.0.0
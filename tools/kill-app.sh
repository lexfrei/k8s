#!/bin/bash

NAMESPACE="argocd"

# Get all applications marked for deletion
apps=$(kubectl get applications.argoproj.io -n $NAMESPACE -o jsonpath='{range .items[?(@.metadata.deletionTimestamp)]}{.metadata.name}{"\n"}{end}')

for app in $apps; do
  echo "Fixing application: $app"
  kubectl patch application "$app" -n $NAMESPACE --type merge -p '{"metadata":{"finalizers":null}}'
done
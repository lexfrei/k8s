#!/usr/bin/env bash

helmfile deps

helmfile apply --suppress-diff --skip-deps

git add ./helmfile.d

git commit -m 'update charts'
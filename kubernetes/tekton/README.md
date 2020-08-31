# Tekton CI

The configurations in this directory are for automating lightwalletd operations using Tekton.

Currently new tags will trigger a docker build and push to https://hub.docker.com/r/electriccoinco/lightwalletd

## Required Pipeline tasks

- git-clone  
  Currently sourced from https://github.com/tektoncd/catalog/tree/master/task/git-clone/0.2
- kaniko  
  Currently sourced from https://github.com/tektoncd/catalog/tree/master/task/kaniko/0.1


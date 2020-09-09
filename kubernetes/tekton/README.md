# Tekton CI
The configurations in this directory are for automating lightwalletd operations using Tekton.

Currently new tags will trigger a docker build and push to https://hub.docker.com/r/electriccoinco/lightwalletd

## Testing 

### Requirements
- `kind` installed
- `docker` installed
- A Docker Hub account (create a new one if you want, its free!)

### Setup

#### Log into Docker Hub  
Just run the command:
```
docker login
```
This creates a `config.json` file that we're going to send to tekton and contains your Docker Hub password!

More info: https://github.com/tektoncd/pipeline/blob/master/docs/auth.md

#### Create a kind cluster
```
kind create cluster --name tekton-testing-zcashd_exporter
```
#### Create a Kubernetes secret containing your Docker hub creds
```
kubectl create secret generic dockerhub-creds \
 --from-file=.dockerconfigjson=~/.docker/config.json \
 --type=kubernetes.io/dockerconfigjson
```

#### Create a service account to use those creds
```
kubectl apply -f serviceaccount.yml
```

#### Install Tekton
```
kubectl apply -f https://storage.googleapis.com/tekton-releases/pipeline/previous/v0.15.2/release.yaml
kubectl apply -f https://github.com/tektoncd/dashboard/releases/download/v0.9.0/tekton-dashboard-release.yaml
```
#### Install the Tekton Catalog tasks
These are predefined tasks from the `tektoncd/catalog` collection on github.

```
kubectl apply -f https://raw.githubusercontent.com/tektoncd/catalog/master/task/git-clone/0.2/git-clone.yaml
kubectl apply -f https://raw.githubusercontent.com/tektoncd/catalog/master/task/kaniko/0.1/kaniko.yaml
```

#### Create the pipeline
```
kubectl apply -f pipeline.yml
```

#### Edit the `PipelineRun`

This object holds all of your pipeline instance parameters.

You **must** edit (unless somehow you got access to my Docker Hub account)  
Change `electriccoinco` to your Docker Hub account name.
```
    - name: dockerHubRepo
      value: electriccoinco/lightwalletd
```
You can also change the `gitTag` and `gitRepositoryURL` values if you want to try building off some other commit, or you fork the code to your own repo.

### Run the pipeline!

You can do this as many times as you want, each one will create a new pipelinerun.
```
kubectl create -f pipelinerun.yml
```


### View the dashboard for status

Forward a port from inside the kubernetes cluster to your laptop.
```
kubectl --namespace tekton-pipelines port-forward svc/tekton-dashboard 9097:9097 &
```

The browse to http://localhost:9097/#/namespaces/default/pipelineruns
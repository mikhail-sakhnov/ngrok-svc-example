# ngrok tunnels as k8s service

Small example to support the blog post.

Custom kubernetes controller for core Service object to expose them as ngrok tunnels to the public.

## Getting secret

Use https://dashboard.ngrok.com/get-started/your-authtoken to get ngrok token.


## Running

`KUBECONFIG=path/to/kubeconfig NGROK_TOKEN=secret make run` 

## Makefile variables
Those values used by example
```
TAG ?= docker.io/soider/ngrok-tunnel-pod
LOCAL_TAG ?= docker.io/soider/ngrok-controller

KUBECONFIG ?= `pwd`/kubeconfig
NGROK_TOKEN ?= ""
```
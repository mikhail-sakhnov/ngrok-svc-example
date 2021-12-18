TAG ?= docker.io/soider/ngrok-tunnel-pod
LOCAL_TAG ?= docker.io/soider/ngrok-controller

KUBECONFIG ?= `pwd`/kubeconfig
NGROK_TOKEN ?= ""

docker-push-pod-image: docker-build-pod-image
	docker push $(TAG) 

docker-build-pod-image: Dockerfile.controller
	docker build -t $(TAG) -f Dockerfile.tunnel .

docker-build-local-image:
	docker build -t $(LOCAL_TAG) -f Dockerfile.controller .

run: 
	echo $(KUBECONFIG)
	docker run -v $(KUBECONFIG):/tmp/kubeconfig  -e KUBECONFIG=/tmp/kubeconfig -e NGROK_TOKEN=$(NGROK_TOKEN) $(LOCAL_TAG) 
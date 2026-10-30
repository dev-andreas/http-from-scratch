IMAGE_NAME = go-server
IMAGE_TAG = local
CLUSTER_NAME = testing-cluster
MONITORING_NAMESPACE = monitoring
HELM_RELEASE_NAME = my-victoria-metrics-k8s-stack
STACK_VERSION = 0.97.0

.PHONY: image create-cluster setup-ingress deploy-local

image:
	@echo "Building Docker image..."
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

create-cluster:
	kind create cluster --name $(CLUSTER_NAME) --config kind-config.yaml

helm-repo:
	@echo "Adding VictoriaMetrics Helm repository..."
	helm repo add victoriametrics https://victoriametrics.github.io/helm-charts/
	helm repo update victoriametrics

deploy-vmstack: helm-repo
	@echo "Installing/Upgrading VictoriaMetrics stack..."
	helm upgrade --install my-victoria-metrics-k8s-stack victoriametrics/victoria-metrics-k8s-stack \
		--version 0.97.0 \
		--namespace monitoring \
		--create-namespace \
		--set kubeControllerManager.enabled=false \
		--set kubeScheduler.enabled=false \
		--set kubeEtcd.enabled=false \
		--set kubeProxy.enabled=false \
		--wait
	@echo "Waiting for VictoriaMetrics CRDs to settle..."
	kubectl wait --for=condition=established --timeout=60s crd/vmservicescrapes.operator.victoriametrics.com || true

setup-ingress:
	@echo "Installing NGINX Ingress Controller..."
	kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
	@echo "Waiting for Ingress Controller to be ready..."
	kubectl wait --namespace ingress-nginx \
	  --for=condition=ready pod \
	  --selector=app.kubernetes.io/component=controller \
	  --timeout=90s

deploy-local: image
	@echo "Loading image into KIND cluster..."
	kind load docker-image $(IMAGE_NAME):$(IMAGE_TAG) --name $(CLUSTER_NAME)
	@echo "Applying Kubernetes manifests..."
	kubectl apply -f k8s/
	@echo "Forcing pod restart to pick up new image..."
	kubectl rollout restart deployment/go-server-deployment
	@echo "Deployment complete. Waiting for pods to be ready..."
	kubectl rollout status deployment/go-server-deployment
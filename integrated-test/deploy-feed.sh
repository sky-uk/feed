#!/usr/bin/env bash
set -e

script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
project_dir=${script_dir}/..
CONTEXT=${CONTEXT:-kind}
NAMESPACE=${NAMESPACE:-feed-test}

function create_namespace(){
  kubectl --context $CONTEXT create ns $NAMESPACE || true
}

function build_fake_aws() {
  local image=$1
  docker image rmi -f ${image}
  docker build -t ${image} fake-aws
  docker push ${image}
  echo "Fake-aws docker image: ${image}"
}

function build_feed() {
  local image=$1
  docker image rmi -f skycirrus/feed-ingress:latest
  make docker -C ${project_dir}
  docker image rmi -f ${image}
  docker tag skycirrus/feed-ingress:latest ${image}
  docker push ${image}
  echo "Feed docker image: ${image}"
}

function deploy_fake_aws() {
  local image=$1
  cat <<EOF | kubectl --context $CONTEXT -n $NAMESPACE apply -f -
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: fake-aws
  namespace: $NAMESPACE
  labels:
    app: fake-aws
spec:
  replicas: 1
  selector:
    matchLabels:
      app: fake-aws
  template:
    metadata:
      labels:
        app: fake-aws
        almost-unique: "$(date +'%H%M%S')"
    spec:
      containers:
      - image: ${image}
        name: fake-aws

        # To make it simpler when developing
        imagePullPolicy: Always

        ports:
        - containerPort: 9000
          protocol: TCP
---
apiVersion: v1
kind: Service
metadata:
  namespace: $NAMESPACE
  labels:
    app: fake-aws
  name: fake-aws
spec:
  # hardcode so feed can talk to it
  ports:
  - port: 80
    protocol: TCP
    targetPort: 9000
  selector:
    app: fake-aws
  type: ClusterIP
EOF
}

function deploy_feed() {
  local image=$1
  local aws_endpoint=$2
  local ingress_class=$3
  cat <<EOF | kubectl --context $CONTEXT -n $NAMESPACE apply -f -
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: feed-ingress
  namespace: $NAMESPACE
  labels:
    app: feed-ingress
spec:
  replicas: 1
  selector:
    matchLabels:
      app: feed-ingress
  template:
    metadata:
      labels:
        app: feed-ingress
        almost-unique: "$(date +'%H%M%S')"
    spec:
      # No need for host network as we do not use ELBs in this test
      # hostNetwork: true

      # delete immediately
      terminationGracePeriodSeconds: 0
      restartPolicy: Always
      serviceAccountName: feed-ingress

      containers:
      - image: ${image}
        # To make it simpler when developing
        imagePullPolicy: Always
        name: feed-ingress
        resources:
          requests:
            cpu: "1"
            memory: 300Mi
          limits:
            memory: 300Mi

        securityContext:
          capabilities:
            add:
            - NET_ADMIN
            - NET_BIND_SERVICE
        env:
          - name: AWS_ACCESS_KEY_ID
            value: "fake-access-key"
          - name: AWS_SECRET_ACCESS_KEY
            value: "fake-secret"
        ports:
        - hostPort: 8080
          containerPort: 8080
          name: ingress
          protocol: TCP
        - hostPort: 8081
          containerPort: 8081
          name: ingress-health
          protocol: TCP
        # Health port of the controller.
        - containerPort: 12082
          name: health
          protocol: TCP

        args:
        - elb
        - --debug
        - --elb-endpoint=${aws_endpoint}
        - --region=does-not-exist
        # 0 to turn off elb attachment validation
        - --elb-expected-number=0
        - --elb-frontend-tag-value=noop-frontend-tag
        - --ingress-class=${ingress_class}
        - --ingress-port=8080
        - --ingress-health-port=8081
        - --health-port=12082
        - --nginx-loglevel=warn
        - --nginx-update-period=5m
        - --access-log
        - --access-log-dir=/var/log/nginx

        # Controller health determines readiness.
        readinessProbe:
          httpGet:
            path: /health
            port: 12082
            scheme: HTTP
          initialDelaySeconds: 1
          timeoutSeconds: 1
          periodSeconds: 1
          failureThreshold: 1

        # Only consider liveness of ingress itself, favouring uptime over controller health.
        livenessProbe:
          httpGet:
            path: /health
            port: 8081
            scheme: HTTP
          initialDelaySeconds: 30
          timeoutSeconds: 1
          periodSeconds: 10
          failureThreshold: 3

        # Access logs volume.
        volumeMounts:
        - name: nginx-log
          mountPath: /var/log/nginx

      volumes:
      - name: nginx-log
        emptyDir: {}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: feed-ingress
  namespace: $NAMESPACE
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: feed-ingress-privileged-psp
  namespace: $NAMESPACE
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: privileged-psp
subjects:
  - kind: ServiceAccount
    name: feed-ingress
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: feed-ingress
rules:
  - apiGroups:
      # Core group for services
      - ""
      # For ingress
      - "extensions"
    resources:
      - ingresses
      - namespaces
      - services
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - "extensions"
    resources:
      - ingresses/status
    verbs:
      - update
---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: feed-ingress
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: feed-ingress
subjects:
  - kind: ServiceAccount
    name: feed-ingress
    namespace: $NAMESPACE
---
apiVersion: v1
kind: Service
metadata:
  name: feed-ingress-admin
spec:
  ports:
  - port: 80
    protocol: TCP
    targetPort: 8081
  selector:
    app: feed-ingress
EOF
}

function create_fake_ingress() {
  local ingress_class=$1
  cat <<EOF | kubectl --context $CONTEXT -n $NAMESPACE apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: $NAMESPACE
spec:
  selector:
    matchLabels:
      app: nginx
  replicas: 1
  template:
    metadata:
      labels:
        app: nginx
    spec:
      # delete immediately
      terminationGracePeriodSeconds: 0
      containers:
      - name: nginx
        image: nginx:1.7.9
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: nginx
spec:
  ports:
  - port: 80
    protocol: TCP
  selector:
    app: nginx
---
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: ${ingress_class}
  labels:
    service: nginx
  name: fake-ingress
  namespace: $NAMESPACE
spec:
  rules:
  - host: fake-ingress.kind.local
    http:
      paths:
      - backend:
          serviceName: nginx
          servicePort: 80
        path: /
EOF
}

function run_tests() {
  local feed_endpoint=$1
  kubectl --context $CONTEXT -n $NAMESPACE delete job/feed-ingress-test --ignore-not-found
  cat <<EOF | kubectl --context $CONTEXT -n $NAMESPACE apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: feed-ingress-test
  labels:
    app: feed-ingress-test
spec:
  activeDeadlineSeconds: 30
  backoffLimit: 0
  completions: 1
  parallelism: 1
  template:
    spec:
      containers:
        - name: feed-ingress-test
          image: curlimages/curl
          # the command will exit successfully when then are no errors
          command:
            - sh
            - -ce
            - 'echo "=== Checking basic_status ==="; curl -I ${feed_endpoint}/basic_status; \
               echo "=== Checking status ==="; curl -I ${feed_endpoint}/status; \
               echo "=== Checking health ==="; curl -I ${feed_endpoint}/health'
          imagePullPolicy: Always
      restartPolicy: Never
EOF

  echo "Waiting for test job to complete"
  set +e
  kubectl --context $CONTEXT -n $NAMESPACE wait --for=condition=complete --timeout=30s job/feed-ingress-test
  job_outcome=$?
  set -e

  if [[ "$job_outcome" != 0 ]]; then
    echo "Test job failed"
    exit 1
  fi
  echo "Test job completed successfully"
}


version=$(git rev-parse HEAD)
registry=localhost:5000
feed_image=${registry}/feed-ingress:v${version}
fake_aws_image=${registry}/fake-aws:v${version}
ingress_class=noop-ingress-class

build_fake_aws ${fake_aws_image}
build_feed ${feed_image}
create_namespace
deploy_fake_aws ${fake_aws_image}
deploy_feed ${feed_image} "http://fake-aws.${NAMESPACE}" ${ingress_class}
create_fake_ingress ${ingress_class}
run_tests "http://feed-ingress-admin.${NAMESPACE}"

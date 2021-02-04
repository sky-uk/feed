#!/usr/bin/env bash
set -e

script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
project_dir=${script_dir}/..
CONTEXT=${CONTEXT:-kind}
NAMESPACE=${NAMESPACE:-feed-test}

function create_namespace() {
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
  local jaeger_agent=$4
  local jaeger_service=$5
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
        - --nginx-opentracing-plugin-path=/usr/local/lib64/libjaegertracing.so #for alpine based images
        - --nginx-opentracing-config-path=/etc/opentracing/jaeger-nginx-config.json

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
        - name: nginx-opentracing
          mountPath: /etc/opentracing

      volumes:
      - name: nginx-log
        emptyDir: {}
      - name: nginx-opentracing
        configMap:
          name: opentracing-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  creationTimestamp: "2021-02-03T14:25:33Z"
  name: opentracing-config
  namespace: $NAMESPACE
data:
  jaeger-nginx-config.json: |
    {
      "service_name": "${jaeger_service}",
      "sampler": {
        "type": "const",
        "param": 1
      },
      "reporter": {
        "localAgentHostPort": "${jaeger_agent}"
      },
      "headers": {
        "jaegerDebugHeader": "jaeger-debug-id",
        "jaegerBaggageHeader": "jaeger-baggage",
        "traceBaggageHeaderPrefix": "uberctx-"
      },
      "baggage_restrictions": {
        "denyBaggageOnInitializationFailure": false,
        "hostPort": ""
      }
    }
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
---
apiVersion: v1
kind: Service
metadata:
  name: feed-ingress
spec:
  ports:
  - port: 80
    protocol: TCP
    targetPort: 8080
  selector:
    app: feed-ingress
EOF

  echo "Waiting for feed-ingress..."
  kubectl  --context $CONTEXT -n $NAMESPACE wait --for=condition=ready --timeout=90s pod -lapp=feed-ingress
}

function create_backend() {
  local ingress_class=$1
  local ingress_host=$2
  cat <<EOF | kubectl --context $CONTEXT -n $NAMESPACE apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend
  namespace: $NAMESPACE
spec:
  selector:
    matchLabels:
      app: backend
  replicas: 1
  template:
    metadata:
      labels:
        app: backend
    spec:
      # delete immediately
      terminationGracePeriodSeconds: 0
      containers:
      - name: backend
        image: nginx:1.7.9
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: backend
spec:
  ports:
  - port: 80
    protocol: TCP
  selector:
    app: backend
---
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: ${ingress_class}
  labels:
    service: backend
  name: fake-ingress
  namespace: $NAMESPACE
spec:
  rules:
  - host: ${ingress_host}
    http:
      paths:
      - backend:
          serviceName: backend
          servicePort: 80
        path: /
EOF

  echo "Waiting for backend..."
  kubectl  --context $CONTEXT -n $NAMESPACE wait --for=condition=ready --timeout=90s pod -lapp=backend
}

function run_tests() {
  local feed_admin_endpoint=$1
  local feed_endpoint=$2
  local target_backend=$3
  local jaeger_api_endpoint=$4
  local jaeger_service=$5
  kubectl --context $CONTEXT -n $NAMESPACE delete job/feed-ingress-test --ignore-not-found
  kubectl --context $CONTEXT -n $NAMESPACE delete configmap feed-test-script --ignore-not-found
  cat <<EOF | kubectl --context $CONTEXT -n $NAMESPACE apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  creationTimestamp: "2021-02-03T14:25:33Z"
  name: feed-tests-script
  namespace: $NAMESPACE
data:
  feed-ingress-tests.sh: |
    #!/bin/sh
    set -e

    echo ""
    echo "=========================="
    echo "=== Feed ingress tests ==="
    echo "=========================="
    echo ""
    echo "Feed ingress admin endpoint: ${feed_admin_endpoint}"
    echo "Feed ingress endpoint: ${feed_endpoint}"
    echo "Jaeger api endpoint: ${jaeger_api_endpoint}"
    echo "Jaeger service: ${jaeger_service}"
    echo "Target backend: ${target_backend}"
    echo ""

    echo "=== Checking basic_status ==="
    curl -s -v -I ${feed_admin_endpoint}/basic_status;

    echo "=== Checking status ==="
    curl -s -v -I ${feed_admin_endpoint}/status

    echo "=== Checking health ==="
    curl -s -v -I ${feed_admin_endpoint}/health

    echo "=== Checking ingress ==="
    curl -s -v -I ${feed_endpoint} -H "Host: ${target_backend}"

    echo "=== Checking traces ==="
    traces=\$(curl -sSf ${jaeger_api_endpoint}/api/traces?service=${jaeger_service} | jq -r ".data[].traceID")
    traces_count=\$(echo \$traces | wc -l)
    if [[ "\$traces_count" == "0" ]];then
       echo "No traces found"
       exit 1
    fi
    echo "Traces found: \$traces"
---
apiVersion: batch/v1
kind: Job
metadata:
  name: feed-ingress-test
  labels:
    app: feed-ingress-test
spec:
  activeDeadlineSeconds: 90
  backoffLimit: 0
  completions: 1
  parallelism: 1
  template:
    spec:
      containers:
        - name: feed-ingress-test
          image: alpine
          # the command will exit successfully when then are no errors
          command:
            - sh
            - -ce
            - "apk update && apk add jq curl && /feed-tests/feed-ingress-tests.sh"
          imagePullPolicy: Always
          volumeMounts:
          - name: feed-tests-script
            mountPath: /feed-tests/
            readOnly: false
      restartPolicy: Never
      volumes:
      - name: feed-tests-script
        configMap:
          name: feed-tests-script
          defaultMode: 0777

EOF

  echo "Waiting for test job to complete"
  set +e
  kubectl --context $CONTEXT -n $NAMESPACE wait --for=condition=complete --timeout=90s job/feed-ingress-test
  job_outcome=$?
  set -e

  if [[ "$job_outcome" != 0 ]]; then
    echo "Test job failed"
    exit 1
  fi
  echo "Test job completed successfully"
}

function deploy_jaeger() {
  local jaeger_name=$1
  kubectl --context $CONTEXT -n $NAMESPACE create -f https://raw.githubusercontent.com/jaegertracing/jaeger-operator/v1.14.0/deploy/crds/jaegertracing.io_jaegers_crd.yaml || true
  kubectl --context $CONTEXT -n $NAMESPACE  apply -f https://raw.githubusercontent.com/jaegertracing/jaeger-operator/v1.14.0/deploy/service_account.yaml
  kubectl --context $CONTEXT -n $NAMESPACE  apply -f https://raw.githubusercontent.com/jaegertracing/jaeger-operator/v1.14.0/deploy/role.yaml
  kubectl --context $CONTEXT -n $NAMESPACE  apply -f https://raw.githubusercontent.com/jaegertracing/jaeger-operator/v1.14.0/deploy/role_binding.yaml
  kubectl --context $CONTEXT -n $NAMESPACE  apply -f https://raw.githubusercontent.com/jaegertracing/jaeger-operator/v1.14.0/deploy/operator.yaml

  echo "Waiting for jaeger operator..."
  kubectl  --context $CONTEXT -n $NAMESPACE wait --for=condition=ready --timeout=90s pod -lname=jaeger-operator

  kubectl --context $CONTEXT apply -n $NAMESPACE -f - <<EOF
apiVersion: jaegertracing.io/v1
kind: Jaeger
metadata:
  name: ${jaeger_name}
EOF

  echo "Waiting for jaeger..."
  kubectl  --context $CONTEXT -n $NAMESPACE wait --for=condition=ready --timeout=90s pod -lapp=jaeger
}

version=$(git rev-parse HEAD)
registry=localhost:5000
feed_image=${registry}/feed-ingress:v${version}
fake_aws_image=${registry}/fake-aws:v${version}
ingress_class=noop-ingress-class
ingress_host=backend.kind.local
jaeger_name=all-in-one-jaeger
jaeger_service=nginx-jaeger-service

build_fake_aws ${fake_aws_image}
build_feed ${feed_image}
create_namespace
create_backend ${ingress_class} ${ingress_host}
deploy_jaeger ${jaeger_name}
deploy_fake_aws ${fake_aws_image}
deploy_feed ${feed_image} "http://fake-aws.${NAMESPACE}" ${ingress_class} "${jaeger_name}-agent:6831" ${jaeger_service}
run_tests "http://feed-ingress-admin.${NAMESPACE}" "http://feed-ingress.${NAMESPACE}" ${ingress_host} "http://${jaeger_name}-query.${NAMESPACE}:16686" ${jaeger_service}

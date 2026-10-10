# casdoor-operator

[![Build](https://github.com/casdoor/casdoor-operator/actions/workflows/build.yml/badge.svg)](https://github.com/casdoor/casdoor-operator/actions/workflows/build.yml)
[![Docker Pulls](https://img.shields.io/docker/pulls/casbin/casdoor-operator.svg)](https://hub.docker.com/r/casbin/casdoor-operator)
[![License](https://img.shields.io/github/license/casdoor/casdoor-operator)](LICENSE)

A Kubernetes operator that runs [Casdoor](https://github.com/casdoor/casdoor) instances. Each `Casdoor` object becomes a Deployment, a Service and, when asked for, an Ingress, a PodDisruptionBudget and a data volume.

What it takes care of:

- Database settings for MySQL, PostgreSQL or SQLite, with passwords read from Secrets. A complete `dataSourceName` can also come from a Secret.
- Redis for sessions. Running more than one replica without Redis is rejected, because users would be signed out whenever a request lands on another pod.
- The initial password of `built-in/admin`, and loading an `init_data.json`.
- Rolling the pods when a referenced Secret changes.
- Health probes on `/api/health`, a PodDisruptionBudget for two or more replicas, and the `scale` subresource, so `kubectl scale` and HorizontalPodAutoscaler work on the `Casdoor` object.
- A `Ready` condition, the running image and the public URL in the status.

## Install

```bash
kubectl apply --server-side -k https://github.com/casdoor/casdoor-operator/config/default
```

This installs the CRD and runs the operator in the `casdoor-operator-system` namespace. It watches all namespaces; pass `--watch-namespace` to the manager to restrict it to one.

## Quick start

A single replica that keeps its data in a SQLite file on a 1Gi volume:

```yaml
apiVersion: operator.casdoor.org/v1alpha1
kind: Casdoor
metadata:
  name: casdoor
spec:
  database:
    driver: sqlite
    storage:
      size: 1Gi
```

```bash
kubectl get casdoor
kubectl port-forward svc/casdoor 8000:8000
```

Open http://localhost:8000. Without `initAdminPasswordSecretRef`, Casdoor asks for the password of `built-in/admin` on the first visit.

## Production example

Three replicas with PostgreSQL, Redis and an Ingress (see [config/samples](config/samples)):

```yaml
apiVersion: operator.casdoor.org/v1alpha1
kind: Casdoor
metadata:
  name: casdoor
spec:
  replicas: 3
  database:
    driver: postgres
    host: postgres.default.svc
    user: casdoor
    name: casdoor
    passwordSecretRef:
      name: casdoor-credentials
      key: db-password
  redis:
    endpoint: redis.default.svc:6379
  initAdminPasswordSecretRef:
    name: casdoor-credentials
    key: admin-password
  ingress:
    className: nginx
    host: door.example.com
    tlsSecretName: door-example-com-tls
```

`origin` defaults to the Ingress host, so the OIDC issuer becomes `https://door.example.com`. Set `spec.origin` when Casdoor is exposed some other way.

## Spec

| Field | Description |
| --- | --- |
| `image` | Casdoor image. Defaults to the version the operator was released with (`--default-image`). |
| `replicas` | Number of pods, 1 by default. More than 1 requires `redis` and a database other than SQLite. |
| `origin` | Public URL, used as the OIDC issuer. |
| `database.driver` | `mysql`, `postgres` or `sqlite`. |
| `database.host`, `port`, `name`, `user`, `sslMode` | Connection settings. `name` defaults to `casdoor`. |
| `database.passwordSecretRef` | Secret key holding the database password. |
| `database.dataSourceNameSecretRef` | Secret key holding a complete `dataSourceName`, replacing the fields above. |
| `database.create` | Create the MySQL or PostgreSQL database if missing. `true` by default. |
| `database.storage` | Volume for the SQLite file. Without it the file lives in an emptyDir and is lost with the pod. The volume is kept when the `Casdoor` object is deleted. |
| `redis.endpoint`, `passwordSecretRef`, `db` | Session store. Several addresses separated by `;` use Redis Cluster. |
| `initAdminPasswordSecretRef` | Password of `built-in/admin` for a new database. |
| `initData.secretRef`, `newOnly` | Secret key holding an `init_data.json`. With `newOnly` (default) existing objects are left alone. |
| `config` | Other `app.conf` items, e.g. `defaultLanguage: zh`. Items covered by the fields above are ignored. |
| `env` | Extra environment variables, applied last. |
| `service.type`, `port`, `annotations` | Service settings. Port 8000 by default. |
| `ingress.host`, `className`, `tlsSecretName`, `annotations` | Creates an Ingress when set. |
| `resources`, `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints`, `priorityClassName`, `serviceAccountName`, `imagePullSecrets`, `podLabels`, `podAnnotations` | Passed to the pod. |

Run `kubectl explain casdoor.spec` for the full schema.

## Development

```bash
make generate   # DeepCopy methods, CRD and ClusterRole
make test       # unit tests and controller tests against a local kube-apiserver (envtest, Linux or macOS)
make build
make run        # run against the cluster in ~/.kube/config
```

## License

[Apache-2.0](LICENSE)

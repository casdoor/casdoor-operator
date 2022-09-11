# casdoor-operator

## Quick Start

1. Create a `yaml` file to define your casdoor instance, for example:

```yaml
apiVersion: operator.casdoor.org/v1
kind: Casdoor
metadata:
  name: casdoor-sample
spec:
  image: casbin/casdoor-all-in-one:latest
```

2. Apply the `yaml` file to your cluster:

```bash
kubectl apply -f casdoor.yaml
```

3. Now let's enjoy Casdoor on K8s!

## How to develop

1. Update CRD yaml

```bash
make manifests
```

2. Install CRD to your cluster

```bash
make install
```

3. Run controller locally

```bash
make run
```
# casdoor-operator

<p align="center">
  <a href="https://hub.docker.com/r/casbin/casdoor-operator">
    <img alt="docker pull casbin/casdoor-operator" src="https://img.shields.io/docker/pulls/casbin/casdoor-operator.svg">
  </a>
  <a href="https://github.com/casdoor/casdoor-operator/actions/workflows/build.yml">
    <img alt="GitHub Workflow Status (branch)" src="https://github.com/casdoor/casdoor-operator/workflows/Build/badge.svg?style=flat-square">
  </a>
  <a href="https://github.com/casdoor/casdoor-operator/releases/latest">
    <img alt="GitHub Release" src="https://img.shields.io/github/v/release/casdoor/casdoor-operator.svg">
  </a>
  <a href="https://hub.docker.com/repository/docker/casbin/casdoor-operator">
    <img alt="Docker Image Version (latest semver)" src="https://img.shields.io/badge/Docker%20Hub-latest-brightgreen">
  </a>
</p>


<p align="center">
  <a href="https://github.com/casdoor/casdoor-operator/blob/master/LICENSE">
    <img src="https://img.shields.io/github/license/casdoor/casdoor-operator?style=flat-square" alt="license">
  </a>
  <a href="https://github.com/casdoor/casdoor-operator/issues">
    <img alt="GitHub issues" src="https://img.shields.io/github/issues/casdoor/casdoor-operator?style=flat-square">
  </a>
  <a href="#">
    <img alt="GitHub stars" src="https://img.shields.io/github/stars/casdoor/casdoor-operator?style=flat-square">
  </a>
  <a href="https://github.com/casdoor/casdoor-operator/network">
    <img alt="GitHub forks" src="https://img.shields.io/github/forks/casdoor/casdoor-operator?style=flat-square">
  </a>
</p>

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
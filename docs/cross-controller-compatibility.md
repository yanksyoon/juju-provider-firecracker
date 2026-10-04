# Disposable Canonical K8s cross-controller compatibility

This repository exposes a bounded, opt-in settings contract for testing a Firecracker provider environment against a disposable Canonical Kubernetes controller/model. It does not implement controller registration, offer/consume, or kubeconfig handling. Those are separate Juju CLI boundaries and must be exercised by the operator with a verified, pinned Juju toolchain.

The provider model-config key is `cross-controller-settings`. Its value is strict JSON:

    {
      "enabled": true,
      "controller_name": "disposable-k8s",
      "api_addresses": ["127.0.0.1:17070"],
      "model_name": "fc-compat",
      "offer_name": "fc-offer",
      "cleanup": true
    }

`api_addresses` must contain host:port values. A CA certificate may be supplied as PEM in `ca_certificate_pem`; invalid PEM is rejected. The contract contains no password, macaroon, token, kubeconfig, or private endpoint default. Credentials and kubeconfig are supplied only through the disposable Juju/Kubernetes tooling and are never copied into provider configuration.

Validation is fail-closed: enabled settings require a controller name, at least one address, a model name or UUID, and an offer name; unknown JSON fields and trailing data are rejected. Disabled settings require no endpoint or model values. Round-trip and malformed-settings tests are in `internal/provider/crosscontroller_test.go` and `internal/provider/config_test.go`.

## Opt-in smoke procedure

Run only on an isolated host with no production controller access. First verify these prerequisites: a disposable Canonical K8s cluster and kubeconfig, a disposable Juju controller/model, a verified pinned Juju client, and network reachability between the two controller endpoints. Do not place kubeconfig or credentials in this repository or in `cross-controller-settings`.

1. Create the disposable Canonical K8s controller/model using the verified `add-k8s` command. Keep the kubeconfig in a temporary file with restrictive permissions.
2. Bootstrap or register the Firecracker controller using only disposable assets and the local provider binary. Do not use production controller names, endpoints, or credentials.
3. Set `cross-controller-settings` with non-secret controller/model/offer metadata and run `juju model-config` validation. The provider must reject malformed JSON before any external operation.
4. Register the disposable controller, then perform the Juju CrossModel offer/consume sequence using the pinned CLI/API. Verify controller registration, model targeting, endpoint/TLS metadata, and offer/consume independently; success at one boundary does not imply the others.
5. Record only command exit status and resource names, never tokens or kubeconfig contents.
6. Clean up in reverse order: remove the consume and offer, destroy the disposable models, destroy both disposable controllers, remove the temporary kubeconfig, and delete any Firecracker VM/network/cgroup state. Re-run `juju controllers`, `juju models`, and the cluster inventory commands to confirm no disposable resource remains.

The smoke procedure is intentionally not an always-on integration test. Without all prerequisites it must stop before registration or mutation. No production access is required or permitted.

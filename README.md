# Wake me Up!

Wake me Upp is an Golang app that listens for Alertmanager Webhooks with alerts, then displays them
nicely with an UI, playing a very loud sound when there are unacknowledged / unresolved alerts.

## Configuration

The configuration for the application is done via command-line flags and a simple YAML configuration
file. The file in `config/config.example.yaml` holds the default values for the app.

### Command line flags

- `-config`: Path to configuration file.

### AlertManager config

Make sure the alert you want are sent to Wake me Up! as an Alertmanager Receiver:

```yaml
apiVersion: monitoring.coreos.com/v1alpha1
kind: AlertmanagerConfig
metadata:
  name: wake-me-up-receiver
  labels:
    alertmanagerConfig: wake-me-up-receiver
  namespace: monitoring
spec:
  route:
    groupBy: ['alertname']
    groupWait: 1s
    groupInterval: 5m
    repeatInterval: 1h
    receiver: wake-me-up
    matchers:
      - name: 'severity'
        value: 'critical'
        matchType: '='
  receivers:
    - name: wake-me-up
      webhook_configs:
        - url: 'http://your-wake-me-up-host:8080/webhook'
          send_resolved: true
```

## Develop

Build binary in local environment:

```sh
make build
```

Run in local environment:

```sh
make run
```

Send mock alert manager webhook payload for testing:

```sh
curl -H "Content-Type: application/json" --data @test/mock-webhook-firing.json http://localhost:8080/webhook
```

### Release Process

The release process is triggered by tags. To trigger a new image build and release, use the
following:

```bash
git tag <version>
git push origin --tags
```

The Docker image should be available at `ghcr.io/ppastorf/wake-me-up` with the tags `<version>` and
`latest`.

Pull requests trigger the pipeline but will not publish the built container image.

# Testing Mock Webhooks

This directory contains sample Alertmanager webhook payloads for testing the application.

## Quick Test with Shell Script

The easiest way to test is using the `test-webhook.sh` script in the root directory:

```bash
# Send a firing alert (will trigger sound)
./test-webhook.sh firing

# Send a resolved alert
./test-webhook.sh resolved
```

## Using curl with JSON Files

You can also use `curl` directly with the JSON files in this directory:

### Send a firing alert:
```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d @test/mock-webhook-firing.json
```

### Send a resolved alert:
```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d @test/mock-webhook-resolved.json
```

### Send multiple alerts at once:
```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d @test/mock-webhook-multiple-alerts.json
```

## Using curl with Inline JSON

You can also send webhooks directly with inline JSON:

### Firing Alert:
```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "version": "4",
    "groupKey": "{}:{alertname=\"TestAlert\"}",
    "status": "firing",
    "receiver": "wake-me-up",
    "groupLabels": {"alertname": "TestAlert"},
    "commonLabels": {"alertname": "TestAlert", "severity": "warning"},
    "commonAnnotations": {"summary": "Test alert"},
    "externalURL": "http://localhost:9093",
    "alerts": [{
      "status": "firing",
      "labels": {"alertname": "TestAlert", "instance": "test:9100"},
      "annotations": {"summary": "This is a test alert"},
      "startsAt": "'$(date -u +"%Y-%m-%dT%H:%M:%S.000Z")'",
      "generatorURL": "http://localhost:9090"
    }]
  }'
```

## Testing Workflow

1. **Start the application:**
   ```bash
   go run cmd/wake-me-up/main.go
   ```

2. **Open the web interface:**
   - Navigate to http://localhost:8080/

3. **Send test webhooks:**
   - Use the script: `./test-webhook.sh firing`
   - Or use curl with the JSON files
   - Watch the web interface update automatically

4. **Verify sound playback:**
   - Firing alerts should trigger a sound
   - Resolved alerts should not trigger a sound

## Customizing Test Payloads

You can modify the JSON files in this directory to test different scenarios:
- Different alert names
- Different severity levels
- Multiple alerts in one webhook
- Different labels and annotations

## Environment Variables

You can customize the webhook URL:
```bash
export WEBHOOK_URL=http://localhost:8080/webhook
./test-webhook.sh firing
```


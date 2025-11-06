# Security Guide for Exposing Webhook to AWS Alertmanager

This guide explains how to securely expose your webhook endpoint to Alertmanager running in AWS with minimal security compromise.

## Security Options

The application supports multiple security mechanisms that can be combined:

### 1. API Key Authentication (Recommended)

**Best for:** Simple, effective authentication that works well with Alertmanager.

**Configuration:**
```yaml
webhook_api_key: "your-secret-api-key-here"
```

**Alertmanager Configuration:**
```yaml
receivers:
  - name: 'wake-me-up'
    webhook_configs:
      - url: 'https://your-domain.com/webhook'
        http_config:
          bearer_token: 'your-secret-api-key-here'
```

Or using header:
```yaml
      - url: 'https://your-domain.com/webhook'
        http_config:
          headers:
            X-API-Key: 'your-secret-api-key-here'
```

**Security Level:** Medium-High
- Prevents unauthorized access
- Easy to implement
- Works well with Alertmanager's built-in authentication

### 2. IP Whitelisting

**Best for:** Restricting access to specific AWS IP ranges or instances.

**Configuration:**
```yaml
allowed_ips:
  - "10.0.0.0/8"        # AWS VPC range
  - "172.16.0.0/12"     # Private IP range
  - "52.1.2.3"          # Specific AWS public IP
```

**How to find AWS IP ranges:**
- **VPC IPs:** Check your VPC CIDR blocks in AWS Console
- **Public IPs:** Use AWS NAT Gateway or Elastic IP addresses
- **EC2 Instance IPs:** Check instance details in AWS Console

**Security Level:** Medium
- Good for network-level filtering
- Can be bypassed if IPs are spoofed
- Best combined with API key

### 3. HTTPS/TLS

**Best for:** Encrypting traffic in transit.

**Configuration:**
```yaml
require_https: true
```

**Setup Options:**

#### Option A: Reverse Proxy (Recommended)
Use nginx, Caddy, or Traefik as a reverse proxy with Let's Encrypt:

```nginx
# nginx example
server {
    listen 443 ssl;
    server_name your-domain.com;
    
    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;
    
    location /webhook {
        proxy_pass http://localhost:8080;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

#### Option B: Go TLS
Modify the application to use `ListenAndServeTLS` (requires certificate management).

**Security Level:** High
- Encrypts all traffic
- Prevents man-in-the-middle attacks
- Required for production

### 4. Combined Approach (Recommended)

For maximum security with minimal complexity:

```yaml
webhook_api_key: "your-strong-random-api-key"
allowed_ips:
  - "10.0.0.0/8"  # Your AWS VPC range
require_https: true
```

## Deployment Options

### Option 1: Direct Internet Exposure (with security)

1. **Generate a strong API key:**
   ```bash
   openssl rand -hex 32
   ```

2. **Configure your application:**
   ```yaml
   webhook_api_key: "generated-key-here"
   require_https: true
   ```

3. **Set up HTTPS** (using reverse proxy or Let's Encrypt)

4. **Configure Alertmanager** with the API key

**Pros:**
- Simple setup
- Works immediately

**Cons:**
- Exposed to internet
- Requires strong API key
- Should use HTTPS

### Option 2: VPN/Tunnel (Most Secure)

Use a VPN or SSH tunnel to connect AWS to your local network:

**SSH Tunnel Example:**
```bash
# On your local machine
ssh -R 8080:localhost:8080 user@your-server

# Alertmanager in AWS connects to your-server:8080
# Traffic is tunneled through SSH
```

**Pros:**
- Most secure
- No internet exposure
- Encrypted by default

**Cons:**
- Requires VPN/tunnel setup
- More complex

### Option 3: AWS PrivateLink / VPC Peering

If you're running the service in AWS:

1. Deploy the service in AWS
2. Use VPC peering or PrivateLink
3. Configure Alertmanager to use private IP

**Pros:**
- No internet exposure
- AWS-native solution
- High performance

**Cons:**
- Requires AWS deployment
- More complex setup

## Recommended Setup for AWS Alertmanager

**Minimal Security (Quick Setup):**
```yaml
webhook_api_key: "strong-random-key-here"
```

**Better Security:**
```yaml
webhook_api_key: "strong-random-key-here"
allowed_ips:
  - "10.0.0.0/8"  # Your AWS VPC CIDR
require_https: true
```

**Alertmanager Configuration:**
```yaml
receivers:
  - name: 'wake-me-up'
    webhook_configs:
      - url: 'https://your-domain.com/webhook'
        http_config:
          bearer_token: 'your-strong-random-key-here'
```

## Testing Security

1. **Test without API key** (should fail):
   ```bash
   curl -X POST http://localhost:8080/webhook \
     -H "Content-Type: application/json" \
     -d @test/mock-webhook-firing.json
   ```

2. **Test with API key** (should succeed):
   ```bash
   curl -X POST http://localhost:8080/webhook \
     -H "Content-Type: application/json" \
     -H "X-API-Key: your-api-key" \
     -d @test/mock-webhook-firing.json
   ```

3. **Test from unauthorized IP** (should fail if IP whitelist enabled)

## Security Best Practices

1. **Use strong API keys:** Generate random keys (32+ characters)
2. **Rotate keys regularly:** Change API keys periodically
3. **Use HTTPS:** Always use HTTPS in production
4. **Monitor logs:** Check logs for unauthorized access attempts
5. **Limit IP ranges:** Use IP whitelisting when possible
6. **Keep software updated:** Regularly update dependencies

## Troubleshooting

**Issue: Webhook rejected with 401 Unauthorized**
- Check API key matches in config and Alertmanager
- Verify header name (X-API-Key or Authorization: Bearer)

**Issue: Webhook rejected with 403 Forbidden**
- Check IP whitelist includes Alertmanager's IP
- Verify X-Forwarded-For headers if behind proxy

**Issue: HTTPS required error**
- Ensure reverse proxy handles TLS
- Or disable require_https for testing

## Example: Complete Secure Setup

**config.yaml:**
```yaml
listen_port: 8080
log_level: info
sound_effect_file_path: "sounds/siren1.wav"
webhook_api_key: "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6"
allowed_ips:
  - "10.0.0.0/8"
require_https: true
```

**Alertmanager config:**
```yaml
receivers:
  - name: 'wake-me-up'
    webhook_configs:
      - url: 'https://your-domain.com/webhook'
        http_config:
          bearer_token: 'a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6'
```

This provides strong security with minimal complexity!


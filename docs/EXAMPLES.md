## 🔧 Configuration Examples

```yaml
# config/config.yaml
server:
  port: 8084
  shutdown_timeout_secs: 30

data:
  file_path: ./config/data/config.json
  persist_interval_secs: 10
  base_url: "https://$1.dev.company.com"

misc:
  gin_mode: release
  scheduling_enabled: true
  scheduling_poll_interval_secs: 60
  scheduling_timezone: "America/Los_Angeles"
  runtime_type: docker
  cors_allowed_origins: "https://app.company.com,https://admin.company.com"
```

### Environment-Specific Overrides

```bash
# Development environment
export GO_SPIN_MISC_GIN_MODE=debug
export GO_SPIN_DATA_PERSIST_INTERVAL_SECS=5
export GO_SPIN_MISC_SCHEDULING_POLL_INTERVAL_SECS=30
export GO_SPIN_MISC_CORS_ALLOWED_ORIGINS="*"

# Production environment
export GO_SPIN_MISC_GIN_MODE=release
export GO_SPIN_DATA_PERSIST_INTERVAL_SECS=60
export GO_SPIN_MISC_SCHEDULING_POLL_INTERVAL_SECS=300
export GO_SPIN_MISC_CORS_ALLOWED_ORIGINS="https://production.company.com"
```

### Security-Focused Configuration

```yaml
server:
  port: 8084
  read_timeout_secs: 30
  write_timeout_secs: 30
  idle_timeout_secs: 60
  shutdown_timeout_secs: 15

misc:
  gin_mode: release
  cors_allowed_origins: "https://trusted-domain.com"
  runtime_type: docker
```

## 📊 Monitoring & Automation Examples

### Health Monitoring Script

```bash
#!/bin/bash
# monitor-go-spin.sh

HOST="localhost:8084"
ALERT_EMAIL="admin@company.com"

# Check main application health
if ! curl -s -f "http://$HOST/health" > /dev/null; then
    echo "ALERT: GoSpin health check failed" | mail -s "GoSpin Alert" $ALERT_EMAIL
    exit 1
fi

# Check if scheduled containers are running when they should be
current_hour=$(date +%H)
if [[ $current_hour -ge 9 && $current_hour -le 17 ]]; then
    # Business hours - check if business containers are running
    business_containers=$(curl -s "http://$HOST/containers" | jq -r '.[] | select(.name | test("business")) | select(.running == false) | .name')
    if [[ -n "$business_containers" ]]; then
        echo "ALERT: Business containers not running during business hours: $business_containers" | \\
            mail -s "Container Schedule Alert" $ALERT_EMAIL
    fi
fi

echo "Monitoring check passed"
```

### Automated Deployment Integration

```bash
#!/bin/bash
# deploy-with-go-spin.sh

# Deploy new version
docker pull myapp:latest

# Update container configuration
curl -X POST http://localhost:8084/container \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "myapp",
    "friendly_name": "My Application v2.0",
    "url": "http://localhost:8080",
    "active": true
  }'

# Restart container
curl -X POST http://localhost:8084/runtime/myapp/stop
sleep 5
curl -X POST http://localhost:8084/runtime/myapp/start

# Verify deployment
if curl -s -f "http://localhost:8080/health"; then
    echo "Deployment successful"
else
    echo "Deployment failed - rolling back"
    docker run -d --name myapp myapp:previous
    exit 1
fi
```

### Backup Automation

```bash
#!/bin/bash
# backup-containers.sh

# Get list of all containers from GoSpin
containers=$(curl -s http://localhost:8084/containers | jq -r '.[].name')

for container in $containers; do
    echo "Backing up container: $container"
    
    # Create container backup
    docker commit "$container" "backup-$container-$(date +%Y%m%d)"
    
    # Export configuration
    curl -s "http://localhost:8084/containers" | \\
        jq ".[] | select(.name == \"$container\")" > "backup-$container-config-$(date +%Y%m%d).json"
done

# Backup GoSpin configuration
cp config/data/config.json "backup-go-spin-$(date +%Y%m%d).json"

echo "Backup completed"
```

## 🌐 Web UI Usage Examples

### Bulk Container Management

1. **Access Web UI**: Navigate to `http://localhost:8084/ui`

2. **Add Multiple Containers**:
   - Click "Add Container" for each service
   - Use consistent naming: `project-service` format
   - Set meaningful friendly names
   - Configure proper URLs for quick access

3. **Create Logical Groups**:
   - Group related containers (e.g., "frontend", "backend", "database")
   - Use groups for batch operations
   - Enable/disable entire stacks at once

4. **Schedule Configuration**:
   - Use visual day selector for clarity
   - Set realistic time windows
   - Test schedules with short intervals first
   - Monitor logs for schedule execution

### URL Generation Patterns

**Base URL Configuration**:
```yaml
data:
  base_url: "https://$1.dev.company.com"
```

**Result Examples**:
- Container "api" → `https://api.dev.company.com`
- Container "frontend" → `https://frontend.dev.company.com`
- Container "docs" → `https://docs.dev.company.com`

**Alternative Patterns**:
```yaml
# Subdirectory pattern
base_url: "https://dev.company.com"
# Result: https://dev.company.com/api

# Port-based pattern
base_url: "http://localhost"
# Result: http://localhost/api (or custom URL if specified)
```

## 🔍 Troubleshooting Examples

### Debug Container Issues

```bash
# Check container status
curl -s http://localhost:8084/runtime/myapp/status | jq .

# View container logs
docker logs myapp

# Check Docker daemon connectivity
docker info

# Verify container exists
docker ps -a | grep myapp

# Test manual start/stop
curl -X POST http://localhost:8084/runtime/myapp/start
curl -X POST http://localhost:8084/runtime/myapp/stop
```


### Performance Investigation

```bash
# Check memory usage
top -p $(pgrep gospin)

# Monitor API response times
time curl http://localhost:8084/containers

# Check file system performance
time ls -la config/data/config.json

# Monitor Docker operations
docker stats
```

These examples provide practical guidance for implementing common scenarios and troubleshooting issues with GoSpin. Adapt the configurations and scripts to match your specific environment and requirements.
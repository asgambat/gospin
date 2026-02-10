# GoSpin Documentation Index

Welcome to the comprehensive documentation for **GoSpin**, a Go application for scheduled management of Docker containers.

## 📑 Documentation Overview

| Document | Description | Target Audience | Last Updated |
|----------|-------------|-----------------|-------------|
| **[README.md](../README.md)** | Main project documentation, quick start guide, and API reference | All users | Current |
| **[DEVELOPMENT.md](DEVELOPMENT.md)** | Development environment setup, testing strategies, and contribution guidelines | Developers, Contributors | Feb 2026 |
| **[DEPLOYMENT.md](DEPLOYMENT.md)** | Production deployment guides, Docker configurations, and operational procedures | DevOps Engineers, System Administrators | Feb 2026 |
| **[ARCHITECTURE.md](ARCHITECTURE.md)** | System architecture, design patterns, and technical deep-dive | Technical Architects, Senior Developers | Feb 2026 |
| **[EXAMPLES.md](EXAMPLES.md)** | Practical examples, use cases, and configuration scenarios | All users, Solution Architects | Feb 2026 |
| **[progetto.txt](progetto.txt)** | Internal architectural overview and patterns (Italian) | Internal team documentation | Current |

## 🎯 Quick Navigation by Role

### 👤 New Users
1. **Start Here**: [README.md - Quick Start](../README.md#-quick-start)
2. **Learn by Example**: [EXAMPLES.md](EXAMPLES.md)
3. **Try the API**: [API Reference](../README.md#-api-reference)
4. **Use the Web UI**: [Web UI Guide](../README.md#-web-ui)

### 💻 Developers
1. **Setup Environment**: [DEVELOPMENT.md - Setup](DEVELOPMENT.md#-setup-development-environment)
2. **Understand Architecture**: [ARCHITECTURE.md](ARCHITECTURE.md)
3. **Learn Patterns**: [ARCHITECTURE.md - Design Patterns](ARCHITECTURE.md#-core-patterns)
4. **Write Tests**: [DEVELOPMENT.md - Testing](DEVELOPMENT.md#-testing-guidelines)
5. **Contribute**: [DEVELOPMENT.md - Contributing](DEVELOPMENT.md#-contributing-guidelines)

### 🛠️ DevOps Engineers
1. **Production Deployment**: [DEPLOYMENT.md - Docker](DEPLOYMENT.md#-docker-deployment)
2. **Security Configuration**: [DEPLOYMENT.md - Security](DEPLOYMENT.md#-reverse-proxy--ssl)
3. **Monitoring Setup**: [DEPLOYMENT.md - Monitoring](DEPLOYMENT.md#-monitoring--observability)
4. **Troubleshooting**: [README.md - Troubleshooting](../README.md#-troubleshooting)
5. **Maintenance**: [DEPLOYMENT.md - Maintenance](DEPLOYMENT.md#-maintenance)

### 🏗️ Architects
1. **System Overview**: [ARCHITECTURE.md - Overview](ARCHITECTURE.md#-system-architecture-overview)
2. **Design Decisions**: [ARCHITECTURE.md - Patterns](ARCHITECTURE.md#-design-principles)
3. **Extension Points**: [ARCHITECTURE.md - Extensions](ARCHITECTURE.md#-extension-points)
4. **Integration Patterns**: [EXAMPLES.md - Use Cases](EXAMPLES.md#-common-use-cases)

## 📋 Document Details

### Main Documentation (README.md)
- **Size**: ~35KB, 900+ lines
- **Content**: Project overview, installation, configuration, API reference
- **Maintenance**: Updated with each release
- **Languages**: English

### Development Guide (DEVELOPMENT.md)
- **Size**: ~25KB, 700+ lines  
- **Content**: Setup instructions, testing strategies, contribution guidelines
- **Target**: Contributors and development team
- **Languages**: English

### Deployment Guide (DEPLOYMENT.md)
- **Size**: ~30KB, 800+ lines
- **Content**: Production deployment, Docker, monitoring, operations
- **Target**: Operations teams and system administrators
- **Languages**: English

### Architecture Guide (ARCHITECTURE.md)
- **Size**: ~20KB, 600+ lines
- **Content**: System design, patterns, component interactions
- **Target**: Technical architects and senior developers
- **Languages**: English

### Examples & Use Cases (EXAMPLES.md)
- **Size**: ~15KB, 400+ lines
- **Content**: Real-world scenarios, configuration examples, scripts
- **Target**: All users seeking practical guidance
- **Languages**: English

### Internal Documentation (progetto.txt)
- **Size**: ~8KB, 140+ lines
- **Content**: Architectural patterns, internal workflows
- **Target**: Internal development team
- **Languages**: Italian

## 🔍 Search & Navigation Tips

### Finding Information
- **API Endpoints**: Search for HTTP method (GET, POST, DELETE) in README.md
- **Configuration Options**: Look for YAML examples or environment variables
- **Error Resolution**: Check Troubleshooting sections in README.md and DEPLOYMENT.md
- **Code Examples**: Found throughout EXAMPLES.md and DEVELOPMENT.md
- **Architecture Patterns**: Detailed in ARCHITECTURE.md with diagrams

### Cross-References
Documents are extensively cross-referenced:
- Links to related sections in other documents
- API examples reference configuration options
- Architecture concepts link to implementation details
- Use cases demonstrate configuration patterns

## 🚀 Getting Started Checklist

### For Development
- [ ] Read [README.md](../README.md) overview
- [ ] Follow [DEVELOPMENT.md setup](DEVELOPMENT.md#-setup-development-environment)
- [ ] Review [ARCHITECTURE.md patterns](ARCHITECTURE.md#-core-patterns)
- [ ] Try [EXAMPLES.md scenarios](EXAMPLES.md#-quick-start-examples)
- [ ] Run tests and contribute

### For Production
- [ ] Understand [README.md features](../README.md#-features)
- [ ] Plan deployment with [DEPLOYMENT.md](DEPLOYMENT.md)
- [ ] Configure security settings
- [ ] Set up monitoring and alerts
- [ ] Test with example scenarios

### For Integration
- [ ] Review [API Reference](../README.md#-api-reference)
- [ ] Import [Postman Collection](go_spin.postman_collection.json)
- [ ] Study [EXAMPLES.md use cases](EXAMPLES.md#-common-use-cases)
- [ ] Test integration scenarios
- [ ] Plan automation workflows

## 📞 Support & Resources

### Community Resources
- **GitHub Repository**: [asgambat/gospin](https://github.com/asgambat/gospin)
- **Issues & Bug Reports**: Use GitHub Issues
- **Feature Requests**: GitHub Discussions
- **API Testing**: Postman Collection included

### Additional Tools
- **Postman Collection**: `go_spin.postman_collection.json`
- **Configuration Examples**: `../config/config.yaml`
- **Docker Compositions**: `../docker-compose.yml`, `../dev.docker-compose.yml`
- **Hot Reload Configs**: `../.air.toml`, `../.air_win.toml`

### Maintenance Schedule
- **README.md**: Updated with each release
- **Technical Docs**: Reviewed quarterly
- **Examples**: Updated when new features are added
- **API Reference**: Synchronized with code changes

---

> 💡 **Tip**: Bookmark this index page for quick access to all documentation. Use browser search (Ctrl+F) to quickly find specific topics across documents.
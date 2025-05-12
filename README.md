# Alethic Instruction-Based State Machine (Streams API)

This component provides a WebSocket and NATS-based streaming API for the Alethic Instruction-Based State Machine (ISM) system. It serves as an interface layer between clients and the ISM processing network.

## Features

- **State Streaming**: Enables clients to receive real-time state updates from the ISM processing network
  - Supports individual state streams (`/ws/stream/:state`)
  - Supports session-specific state streams (`/ws/stream/:state/:session`)
- **Data Source Integration**: Allows external data sources to connect to the ISM network
  - Provides a WebSocket interface for data sources (`/ws/ds/:ds`)
  - Implements request-reply pattern for data source queries

## Architecture

- **WebSocket Connections**: Clients connect via WebSockets to stream data or provide data source capabilities
- **NATS Messaging**: Uses NATS for high-performance, scalable messaging between components
- **Connection Pools**: Manages groups of WebSocket connections associated with specific NATS subjects
- **Proxy Pattern**: Implements a proxy layer that bridges WebSocket connections with NATS messaging

## API Endpoints

- **`/ws/stream/:state`**: Connect to stream updates for a specific state
- **`/ws/stream/:state/:session`**: Connect to stream updates for a specific state and session
- **`/ws/ds/:ds`**: Connect as a data source provider

## Deployment

### Local Development

The service can be deployed as a standalone Go application:

```bash
# Run locally
go run main.go

# Build
go build -o stream-api main.go
```

### Docker and Kubernetes

Use the provided Makefile commands for Docker-based deployment:

```bash
# Build the Docker image
make build

# Override the default image name
make build IMAGE=your-registry/alethic-ism-stream-api:latest

# Clean up old Docker images and containers
make clean

# Bump patch version and create git tag
make version
```

For Kubernetes deployment, configuration is available in the `k8s` directory. Use:

```bash
kubectl apply -f k8s/deployment.yaml
```

## Configuration

Configuration is managed through environment variables:

- `NATS_URL`: URL of the NATS server (default: `nats://localhost:4222`)

## License
Alethic ISM is under a DUAL licensing model, please refer to [LICENSE.md](LICENSE.md).

**AGPL v3**  
Intended for academic, research, and nonprofit institutional use. As long as all derivative works are also open-sourced under the same license, you are free to use, modify, and distribute the software.

**Commercial License**
Intended for commercial use, including production deployments and proprietary applications. This license allows for closed-source derivative works and commercial distribution. Please contact us for more information.
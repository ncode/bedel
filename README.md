# Bedel 
![Logo](logo.png)

[![Go](https://github.com/ncode/bedel/actions/workflows/go.yml/badge.svg)](https://github.com/ncode/bedel/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ncode/bedel)](https://goreportcard.com/report/github.com/ncode/bedel)
[![codecov](https://codecov.io/gh/ncode/bedel/graph/badge.svg?token=N98KAO33K5)](https://codecov.io/gh/ncode/bedel)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

`bedel` is a utility designed to synchronize ACLs across multiple nodes in Redis and Redis-compatible databases like Valkey. It specifically addresses the challenge of managing users created outside the traditional configuration file, such as those generated through the [Vault database backend](https://www.vaultproject.io/docs/secrets/databases/redis). By keeping ACLs consistent across all nodes, Bedel ensures seamless user management and enhanced security in distributed environments. For more information on the underlying issue with Redis, see [Redis Issue #7988](https://github.com/redis/redis/issues/7988).

## Table of Contents

- [Features](#features)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
  - [Running the Tests](#running-the-tests)
- [Development Setup](#development-setup)
  - [Configuration](#configuration)
- [Usage](#usage)
- [Contributing](#contributing)
- [License](#license)

## Features


- Automated User Synchronization: Automatically synchronizes Redis users and ACLs across all nodes to maintain consistency.
- Vault Integration: Seamlessly integrates with HashiCorp Vault's database backend for dynamic user management.
- Configurable Sync Intervals: Allows customization of synchronization intervals to suit your deployment needs.
- Lightweight and Efficient: Designed to have minimal impact on performance, even with thousands of users.
- Easy Deployment: Simple to deploy with Docker Compose or as a standalone binary.
- Robust Logging: Provides detailed logs for monitoring and troubleshooting.

## Getting Started

These instructions will guide you through getting a copy of `bedel` up and running on your system for development and testing purposes.

### Prerequisites


For users:
- Redis server setup
- Access to Vault database backend (if using Vault for user generation).

For developers:
- Go 1.21 or higher
- Docker and Docker Compose (for development and testing).
- Git (for cloning the repository).

### Installing

Follow these steps to get a development environment running:

1. Clone the repository:
```bash
$ git clone https://github.com/ncode/bedel.git
$ cd bedel
$ go build
```

2. Go install:
```bash
4 go install github.com/ncode/bedel/cmd/bedel@latest
```

### Running the Tests

To run the automated tests for this system, use the following command:

```bash
$ go test ./...
```

## Development Setup

Bedel comes with a development environment setup using Docker Compose. This setup includes:

- Three Redis instances (redis0001, redis0002, redis0003)
- Three Bedel instances (bedel_redis0001, bedel_redis0002, bedel_redis0003)
- A Vault instance for managing secrets

To start the development environment:

1. Ensure you have Docker and Docker Compose installed.
2. Navigate to the project root directory.
3. Run the following command:

```bash
$ cd config/development
$ make
```

This will start all the services defined in the `docker-compose.yaml` file.

### Configuration

The `docker-compose.yaml` file contains the configuration for all services. Here are some key points:

- Redis instances are configured with custom configuration files located in the `./redis` directory.
- Bedel instances are configured to connect to their respective Redis instances.
- The Vault instance is set up with a root token "root" and listens on port 8200.

## Usage

Bedel can be run in two modes:

### 1. Run Once Mode

Performs a single synchronization of ACLs from the primary Redis node to the replica.

```bash
$ bedel runOnce -a <redis-address> -p <password> -u <username>
```

### 2. Continuous Loop:

Continuously synchronizes ACLs at a defined interval.

```bash
$ bedel run -a <redis-address> -p <password> -u <username> --sync-interval <duration>
```

For more options and commands, run:
```bash
$ bedel --help
```

### Configuration file

Bedel can also read configurations from a YAML file (default: $HOME/.bedel.yaml). Command-line options override configurations in the file.

Example Configuration File (~/.bedel.yaml):
```yaml
address: localhost:6379
password: mypassword
username: default
syncInterval: 10s
logLevel: INFO
aclfile: false
```


## Contributing

Contributions are welcome!

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

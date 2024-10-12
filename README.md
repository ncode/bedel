# Bedel 

[![Go](https://github.com/ncode/port53/actions/workflows/go.yml/badge.svg)](https://github.com/ncode/port53/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ncode/bedel)](https://goreportcard.com/report/github.com/ncode/bedel)
[![codecov](https://codecov.io/gh/ncode/bedel/graph/badge.svg?token=N98KAO33K5)](https://codecov.io/gh/ncode/bedel)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

`bedel` is a tool designed to address a specific challenge with Redis: synchronizing users generated outside of the configuration file, such as through the Vault database backend. This utility ensures that Redis ACLs are up-to-date and consistent across all nodes. More info [here](https://github.com/redis/redis/issues/7988).

## Features

- Synchronize Redis users created outside the traditional config file.
- Integration with Vault database backend for user management.
- Automated and consistent user synchronization.
- Easy to deploy and integrate within existing Redis setups.

## Getting Started

These instructions will guide you through getting a copy of `bedel` up and running on your system for development and testing purposes.

### Prerequisites

- Redis server setup.
- Access to Vault database backend (if using Vault for user generation).
- Go environment for development.
- Docker and Docker Compose (for development setup).

### Installing

Follow these steps to get a development environment running:

1. Clone the repository:
```bash
$ git clone https://github.com/ncode/bedel.git
$ cd bedel
$ go build
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
$ docker-compose up -d
```

This will start all the services defined in the `docker-compose.yaml` file.

### Configuration

The `docker-compose.yaml` file contains the configuration for all services. Here are some key points:

- Redis instances are configured with custom configuration files located in the `./redis` directory.
- Bedel instances are configured to connect to their respective Redis instances.
- The Vault instance is set up with a root token "root" and listens on port 8200.

### Valkey

Valkey is not directly mentioned in the provided files, but it's likely a tool or component related to key management or validation in the context of this project. If you have more information about Valkey, please provide it, and I'll add it to the README.

## Usage

Bedel can be run in two modes:

1. Run Once:
```bash
$ bedel runOnce -a <redis-address> -p <password> -u <username>
```

2. Continuous Loop:
```bash
$ bedel run -a <redis-address> -p <password> -u <username>
```

For more options and commands, run:
```bash
$ bedel --help
```

## Contributing

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct, and the process for submitting pull requests to us.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
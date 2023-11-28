# Bedel 

[![Go](https://github.com/ncode/port53/actions/workflows/go.yml/badge.svg)](https://github.com/ncode/port53/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ncode/bedel)](https://goreportcard.com/report/github.com/ncode/bedel)
[![codecov](https://codecov.io/gh/ncode/bedel/graph/badge.svg?token=N98KAO33K5)](https://codecov.io/gh/ncode/bedel)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

`bedel` is a tool designed to address a specific challenge with Redis: synchronizing users generated outside of the configuration file, such as through the Vault database backend. This utility ensures that Redis acls are up-to-date and consistent across all nodes. More info [here](https://github.com/redis/redis/issues/7988).

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
  

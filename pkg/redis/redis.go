package redis

import (
	"bufio"
	"context"
	"regexp"
	"strings"

	"github.com/redis/go-redis/v9"
)

type NodeInfo struct {
	Host     string
	Port     string
	Function string
}

func getRedisInfo(client *redis.Client, section string) (response string, err error) {
	response, err = client.Info(context.Background(), section).Result()
	if err != nil {
		return "", err
	}
	return response, err
}

func parseRedisOutput(output string) (nodes []NodeInfo, err error) {
	slaveRegex := regexp.MustCompile(`slave\d+:ip=(?P<ip>\d+\.\d+\.\d+\.\d+),port=(?P<port>\d+)`)
	masterHostRegex := regexp.MustCompile(`master_host:(?P<host>[a-zA-Z0-9\.-_]+)`)
	masterPortRegex := regexp.MustCompile(`master_port:(?P<port>\d+)`)

	var masterHost, masterPort string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		if matches := masterHostRegex.FindStringSubmatch(line); matches != nil {
			masterHost = matches[1]
		}

		if matches := masterPortRegex.FindStringSubmatch(line); matches != nil {
			masterPort = matches[1]
			nodes = append(nodes, NodeInfo{Host: masterHost, Port: masterPort, Function: "master"})
		}

		if matches := slaveRegex.FindStringSubmatch(line); matches != nil {
			ip := matches[slaveRegex.SubexpIndex("ip")]
			port := matches[slaveRegex.SubexpIndex("port")]
			nodes = append(nodes, NodeInfo{Host: ip, Port: port, Function: "slave"})
		}
	}

	if err := scanner.Err(); err != nil {
		return nodes, err
	}

	return nodes, err
}

func FindNodes(addr string, username string, password string) (nodes []NodeInfo, err error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Username: username,
		Password: password,
	})

	replicationInfo, err := getRedisInfo(rdb, "replication")
	if err != nil {
		return nodes, err
	}
	nodes, err = parseRedisOutput(replicationInfo)
	if err != nil {
		return nodes, err
	}

	return nodes, err
}

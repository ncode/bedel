package redis

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/redis/go-redis/v9"
)

type NodeInfo struct {
	IPAddress string
	Port      string
	Function  string
}

func getRedisInfo(client *redis.Client, section string) string {
	res, err := client.Info(context.Background(), section).Result()
	if err != nil {
		fmt.Println("Error fetching info from Redis:", err)
		return ""
	}
	return res
}

func parseRedisOutput(output string) []NodeInfo {
	var nodes []NodeInfo

	slaveRegex := regexp.MustCompile(`slave\d+:ip=(?P<ip>\d+\.\d+\.\d+\.\d+),port=(?P<port>\d+)`)
	masterHostRegex := regexp.MustCompile(`master_host:(?P<host>[a-zA-Z0-9\.-_]+)`)
	masterPortRegex := regexp.MustCompile(`master_port:(?P<port>\d+)`)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "role:") {
			role := strings.TrimPrefix(line, "role:")
			if role == "slave" {
				hostMatch := masterHostRegex.FindStringSubmatch(line)
				portMatch := masterPortRegex.FindStringSubmatch(line)

				if hostMatch != nil && portMatch != nil {
					ip := hostMatch[1]
					port := portMatch[1]
					nodes = append(nodes, NodeInfo{IPAddress: ip, Port: port, Function: "master"})
				}
			}
		}

		if matches := slaveRegex.FindStringSubmatch(line); matches != nil {
			ip := matches[slaveRegex.SubexpIndex("ip")]
			port := matches[slaveRegex.SubexpIndex("port")]
			nodes = append(nodes, NodeInfo{IPAddress: ip, Port: port, Function: "slave"})
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading output:", err)
	}

	return nodes
}

func Run() {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6380", // change this to your Redis address
	})

	replicationInfo := getRedisInfo(rdb, "replication")
	nodes := parseRedisOutput(replicationInfo)
	for _, node := range nodes {
		fmt.Println(node)
	}
}

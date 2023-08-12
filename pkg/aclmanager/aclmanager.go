package aclmanager

import (
	"bufio"
	"context"
	"log"
	"regexp"
	"strings"

	"github.com/redis/go-redis/v9"
)

// AclManager containers the struct for bedel to manager the state of aclmanager acls
type AclManager struct {
	Addr        string
	Username    string
	Password    string
	RedisClient *redis.Client
}

// New creates a new AclManager
func New(addr string, username string, password string) *AclManager {
	redisClient := redis.NewClient(&redis.Options{
		Addr:     addr,
		Username: username,
		Password: password,
	})
	return &AclManager{
		Addr:        addr,
		Username:    username,
		Password:    password,
		RedisClient: redisClient,
	}
}

type NodeInfo struct {
	Host     string
	Port     string
	Function string
}

func parseRedisOutput(output string) (nodes []NodeInfo, err error) {
	slaveRegex := regexp.MustCompile(`slave\d+:ip=(?P<ip>.+),port=(?P<port>\d+)`)
	masterHostRegex := regexp.MustCompile(`master_host:(?P<host>.+)`)
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

// FindNodes returns a list of nodes in the cluster based on the redis info replication command
func (a *AclManager) FindNodes() (nodes []NodeInfo, err error) {
	replicationInfo, err := a.RedisClient.Info(context.Background(), "replication").Result()
	if err != nil {
		return nodes, err
	}

	nodes, err = parseRedisOutput(replicationInfo)
	if err != nil {
		return nodes, err
	}

	return nodes, err
}

// ListAcls returns a list of acls in the cluster based on the redis acl list command
func (a *AclManager) ListAcls() (acls []string, err error) {
	result, err := a.RedisClient.Do(context.Background(), "ACL", "LIST").Result()
	if err != nil {
		return acls, err
	}

	aclList, ok := result.([]interface{})
	if !ok {
		log.Fatal("Error: Unexpected result format.")
	}

	for _, acl := range aclList {
		acls = append(acls, acl.(string))
	}

	return acls, err
}

// Close closes the redis client
func (a *AclManager) Close() error {
	return a.RedisClient.Close()
}

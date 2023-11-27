package aclmanager

import (
	"bufio"
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"regexp"
	"strings"
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
	Address  string
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
			nodes = append(nodes, NodeInfo{Address: fmt.Sprintf("%s:%s", masterHost, masterPort), Function: "master"})
		}

		if matches := slaveRegex.FindStringSubmatch(line); matches != nil {
			ip := matches[slaveRegex.SubexpIndex("ip")]
			port := matches[slaveRegex.SubexpIndex("port")]
			nodes = append(nodes, NodeInfo{Address: fmt.Sprintf("%s:%s", ip, port), Function: "slave"})
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

// SyncAcls connects to master node and syncs the acls to the current node
func (a *AclManager) SyncAcls() (err error) {
	nodes, err := a.FindNodes()
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if node.Function == "master" {
			if a.Addr == node.Address {
				return err
			}
			if err != nil {
				return fmt.Errorf("error listing master acls: %v", err)
			}
			master := redis.NewClient(&redis.Options{
				Addr:     node.Address,
				Username: a.Username,
				Password: a.Password,
			})
			defer master.Close()

			_, err := syncAcls(master, a.RedisClient)
			if err != nil {
				return fmt.Errorf("error syncing acls: %v", err)
			}
		}
	}

	return err
}

// Close closes the redis client
func (a *AclManager) Close() error {
	return a.RedisClient.Close()
}

// listAcls returns a list of acls in the cluster based on the redis acl list command
func listAcls(client *redis.Client) (acls []string, err error) {
	result, err := client.Do(context.Background(), "ACL", "LIST").Result()
	if err != nil {
		return acls, err
	}

	aclList, ok := result.([]interface{})
	if !ok {
		return acls, fmt.Errorf("expected type result format: %v", result)
	}

	length := len(aclList)
	if length == 0 {
		return acls, err
	}

	for _, acl := range aclList {
		value, ok := acl.(string)
		if !ok {
			return acls, fmt.Errorf("expected type string: %v", acl)
		}

		acls = append(acls, value)
	}

	return acls, err
}

// syncAcls returns a list of acls in the cluster based on the redis acl list command
func syncAcls(source *redis.Client, destination *redis.Client) (deleted []string, err error) {
	sourceAcls, err := listAcls(source)
	if err != nil {
		return nil, fmt.Errorf("error listing master acls: %v", err)
	}

	destinationAcls, err := listAcls(destination)
	if err != nil {
		return nil, fmt.Errorf("error listing current acls: %v", err)
	}

	// Map to keep track of ACLs to add
	toAdd := make(map[string]struct{})
	for _, acl := range sourceAcls {
		toAdd[acl] = struct{}{}
	}

	// Delete ACLs not in source and remove from the toAdd map if present in destination
	for _, acl := range destinationAcls {
		if _, found := toAdd[acl]; found {
			// If found in source, don't need to add, so remove from map
			delete(toAdd, acl)
		} else {
			// If not found in source, delete from destination
			if err := destination.Do(context.Background(), "ACL", "DELUSER", acl).Err(); err != nil {
				return deleted, fmt.Errorf("error deleting acl: %v", err)
			}
			deleted = append(deleted, acl)
		}
	}

	// Add remaining ACLs from source
	for acl := range toAdd {
		if err := destination.Do(context.Background(), "ACL", "SETUSER", acl).Err(); err != nil {
			return deleted, fmt.Errorf("error setting acl: %v", err)
		}
	}

	return deleted, nil
}

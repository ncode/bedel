package aclmanager

import (
	"bufio"
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"log/slog"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

var logger = slog.New(
	slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{
			AddSource: true,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == slog.SourceKey {
					s := a.Value.Any().(*slog.Source)
					s.File = path.Base(s.File)
				}
				return a
			},
		},
	),
)

var (
	slaveRegex      = regexp.MustCompile(`slave\d+:ip=(?P<ip>.+),port=(?P<port>\d+)`)
	masterHostRegex = regexp.MustCompile(`master_host:(?P<host>.+)`)
	masterPortRegex = regexp.MustCompile(`master_port:(?P<port>\d+)`)
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
	slog.Debug("Finding nodes")
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
	logger.Debug("Syncing acls")
	nodes, err := a.FindNodes()
	if err != nil {
		return err
	}

	ctx := context.Background()
	for _, node := range nodes {
		if node.Function == "master" {
			if a.Addr == node.Address {
				return err
			}
			master := redis.NewClient(&redis.Options{
				Addr:     node.Address,
				Username: a.Username,
				Password: a.Password,
			})
			defer master.Close()

			_, err := mirrorAcls(ctx, master, a.RedisClient)
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
func listAcls(ctx context.Context, client *redis.Client) (acls []string, err error) {
	result, err := client.Do(ctx, "ACL", "LIST").Result()
	if err != nil {
		return nil, err
	}

	aclList, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result format: %v", result)
	}

	if len(aclList) == 0 {
		return nil, nil // Return nil if no ACLs are found
	}

	acls = make([]string, len(aclList))
	for i, acl := range aclList {
		aclStr, ok := acl.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected type for ACL: %v", acl)
		}
		acls[i] = aclStr
	}

	return acls, nil
}

// mirrorAcls returns a list of acls in the cluster based on the redis acl list command
func mirrorAcls(ctx context.Context, source *redis.Client, destination *redis.Client) (deleted []string, err error) {
	slog.Debug("Mirroring acls")
	sourceAcls, err := listAcls(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("error listing source acls: %v", err)
	}

	destinationAcls, err := listAcls(ctx, destination)
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
		user := strings.Split(acl, " ")[1]
		if _, found := toAdd[acl]; found {
			// If found in source, don't need to add, so remove from map
			delete(toAdd, acl)
			logger.Debug("ACL for %s already in sync", "acl", user)
		} else {
			// If not found in source, delete from destination
			logger.Info("Deleting ACL for %s", user)
			if err := destination.Do(context.Background(), "ACL", "DELUSER", acl).Err(); err != nil {
				return deleted, fmt.Errorf("error deleting acl: %v", err)
			}
			deleted = append(deleted, acl)
		}
	}

	// Add remaining ACLs from source
	for acl := range toAdd {
		user := strings.Split(acl, " ")[1]
		logger.Info("Syncing ACL for %s", user)
		if err := destination.Do(context.Background(), "ACL", "SETUSER", acl).Err(); err != nil {
			return deleted, fmt.Errorf("error setting acl: %v", err)
		}
	}

	return deleted, nil
}

// Loop loops through the sync interval and syncs the acls
func (a *AclManager) Loop(ctx context.Context) (err error) {
	ticker := time.NewTicker(viper.GetDuration("syncInterval") * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return err
		case <-ticker.C:
			err = a.SyncAcls()
			if err != nil {
				return fmt.Errorf("error syncing acls: %v", err)
			}
		}
	}
}

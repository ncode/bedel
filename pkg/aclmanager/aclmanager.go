package aclmanager

import (
	"bufio"
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const (
	Primary = iota
	Follower
	Unknown
)

var (
	followerRegex    = regexp.MustCompile(`slave\d+:ip=(?P<ip>.+),port=(?P<port>\d+)`)
	primaryHostRegex = regexp.MustCompile(`master_host:(?P<host>.+)`)
	primaryPortRegex = regexp.MustCompile(`master_port:(?P<port>\d+)`)
	role             = regexp.MustCompile(`role:master`)
	filterUser       = regexp.MustCompile(`^user\s+`)
)

// AclManager containers the struct for bedel to manager the state of aclmanager acls
type AclManager struct {
	Addr        string
	Username    string
	Password    string
	RedisClient *redis.Client
	primary     atomic.Bool
	nodes       map[string]int
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
		nodes:       make(map[string]int),
	}
}

// findNodes returns a list of nodes in the cluster based on the redis info replication command
func (a *AclManager) findNodes(ctx context.Context) (err error) {
	slog.Debug("Finding nodes")
	replicationInfo, err := a.RedisClient.Info(ctx, "replication").Result()
	if err != nil {
		return err
	}

	a.primary.Store(role.MatchString(replicationInfo))

	var masterHost, masterPort string
	var nodes []string
	scanner := bufio.NewScanner(strings.NewReader(replicationInfo))
	for scanner.Scan() {
		line := scanner.Text()

		slog.Debug("Parsing line looking for masterHost", "content", line)
		if matches := primaryHostRegex.FindStringSubmatch(line); matches != nil {
			masterHost = matches[1]
		}

		slog.Debug("Parsing line looking for Follower", "content", line)
		if matches := primaryPortRegex.FindStringSubmatch(line); matches != nil {
			masterPort = matches[1]
			nodes = append(nodes, fmt.Sprintf("%s:%s", masterHost, masterPort))
			a.nodes[fmt.Sprintf("%s:%s", masterHost, masterPort)] = Primary
		}

		if matches := followerRegex.FindStringSubmatch(line); matches != nil {
			ip := matches[followerRegex.SubexpIndex("ip")]
			port := matches[followerRegex.SubexpIndex("port")]
			nodes = append(nodes, fmt.Sprintf("%s:%s", ip, port))
			a.nodes[fmt.Sprintf("%s:%s", ip, port)] = Follower
		}
	}

	for _, node := range nodes {
		if _, ok := a.nodes[node]; !ok {
			delete(a.nodes, node)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return err
}

// CurrentFunction check if the current node is the Primary node
func (a *AclManager) CurrentFunction(ctx context.Context) (function int, err error) {
	slog.Debug("Check node current function")
	err = a.findNodes(ctx)
	if err != nil {
		return Unknown, err
	}
	if a.primary.Load() {
		return Primary, err
	}

	return Follower, err
}

func (a *AclManager) Primary(ctx context.Context) (primary *AclManager, err error) {
	err = a.findNodes(ctx)
	if err != nil {
		return nil, err
	}

	for address, function := range a.nodes {
		if function == Primary {
			return New(address, a.Username, a.Username), err
		}
	}

	return nil, err
}

// SyncAcls connects to master node and syncs the acls to the current node
func (a *AclManager) SyncAcls(ctx context.Context, primary *AclManager) (added []string, deleted []string, err error) {
	slog.Debug("Syncing acls")
	if primary == nil {
		return added, deleted, fmt.Errorf("no primary found")
	}
	added, deleted, err = mirrorAcls(ctx, primary.RedisClient, a.RedisClient)
	if err != nil {
		return added, deleted, fmt.Errorf("error syncing acls: %v", err)
	}

	return added, deleted, err
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
func mirrorAcls(ctx context.Context, source *redis.Client, destination *redis.Client) (added []string, deleted []string, err error) {
	slog.Debug("Mirroring acls")
	sourceAcls, err := listAcls(ctx, source)
	if err != nil {
		return nil, nil, fmt.Errorf("error listing source acls: %v", err)
	}

	destinationAcls, err := listAcls(ctx, destination)
	if err != nil {
		return nil, nil, fmt.Errorf("error listing current acls: %v", err)
	}

	// Map to keep track of ACLs to add
	toAdd := make(map[string]struct{})
	for _, acl := range sourceAcls {
		toAdd[acl] = struct{}{}
	}

	// Delete ACLs not in source and remove from the toAdd map if present in destination
	var insync uint
	for _, acl := range destinationAcls {
		username := strings.Split(acl, " ")[1]
		if _, found := toAdd[acl]; found {
			// If found in source, don't need to add, so remove from map
			delete(toAdd, acl)
			slog.Debug("ACL already in sync", "username", username)
			insync++
		} else {
			// If not found in source, delete from destination
			slog.Debug("Deleting ACL", "username", username)
			if err := destination.Do(context.Background(), "ACL", "DELUSER", username).Err(); err != nil {
				return nil, nil, fmt.Errorf("error deleting acl: %v", err)
			}
			deleted = append(deleted, username)
		}
	}

	// Add remaining ACLs from source
	for acl := range toAdd {
		username := strings.Split(acl, " ")[1]
		slog.Debug("Syncing ACL", "username", username, "line", acl)
		command := strings.Split(filterUser.ReplaceAllString(acl, "ACL SETUSER "), " ")
		commandInterfce := make([]interface{}, len(command))
		for i, s := range command {
			commandInterfce[i] = s
		}
		if err := destination.Do(context.Background(), commandInterfce...).Err(); err != nil {
			return nil, nil, fmt.Errorf("error setting acl: %v", err)
		}
		added = append(added, username)
	}

	return added, deleted, nil
}

// Loop loops through the sync interval and syncs the acls
func (a *AclManager) Loop(ctx context.Context) (err error) {
	ticker := time.NewTicker(viper.GetDuration("syncInterval") * time.Second)
	defer ticker.Stop()

	var primary *AclManager
	for {
		select {
		case <-ctx.Done():
			return err
		case <-ticker.C:
			function, e := a.CurrentFunction(ctx)
			if err != nil {
				slog.Warn("unable to check if it's a Primary", "message", e)
				err = fmt.Errorf("unable to check if it's a Primary: %w", e)
			}
			if function == Follower {
				primary, err = a.Primary(ctx)
				if err != nil {
					slog.Warn("unable to find Primary", "message", e)
					continue
				}
				var added, deleted []string
				added, deleted, err = a.SyncAcls(ctx, primary)
				if err != nil {
					slog.Warn("unable to sync acls from Primary", "message", e)
					err = fmt.Errorf("unable to sync acls from Primary: %w", e)
					continue
				}
				slog.Info("Synced acls from Primary", "added", added, "deleted", deleted)
			}
		}
	}
}
